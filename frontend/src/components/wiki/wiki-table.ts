// Table extension with the column-width defaults split apart.
//
// Tiptap's Table extension exposes a single `cellMinWidth`, which it spends on
// three different jobs at once:
//
//   1. the floor a drag may shrink a column to,
//   2. the width assumed for a column nobody has dragged yet, and
//   3. a clamp applied to an explicit column width when rendering it.
//
// Job 2 is what decides whether a wide table overflows. prosemirror-tables sums
// it across the columns and writes the total as an inline `min-width` on the
// <table>, and that total is the only thing that can beat the `width: 100%` in
// wiki-editor.css. At the stock 25 a twenty-column table asks for 500px, loses
// to `width: 100%`, and every column collapses to about one letter with nothing
// for .tableWrapper to scroll.
//
// Raising `cellMinWidth` alone fixes job 2 but breaks jobs 1 and 3: columns
// could no longer be dragged narrower than the new default, and one that had
// already been dragged narrow would be clamped back up on render. So the three
// are separated here — a 120px default width, a 25px floor — which needs a
// custom node view (jobs 2 and 3 live in the view that writes the <colgroup>)
// and a custom columnResizing call (job 1 and the live-drag preview).

import { mergeAttributes } from "@tiptap/core"
import { Table, type TableOptions } from "@tiptap/extension-table"
import type { Node as ProseMirrorNode } from "@tiptap/pm/model"
import { columnResizing, tableEditing } from "@tiptap/pm/tables"
import type { NodeView, ViewMutationRecord } from "@tiptap/pm/view"

/** Width assumed for a column that has never been dragged. Also the width such
 *  a column ends up rendering at, since the table's total is divided evenly
 *  among the unsized columns under `table-layout: fixed`. */
export const WIKI_COLUMN_DEFAULT_WIDTH = 120

/** How narrow a drag may make a column. Deliberately far below the default —
 *  index, flag and checkbox columns are the reason someone reaches for the
 *  resize handle in the first place. */
export const WIKI_COLUMN_MIN_WIDTH = 25

/** The width attributes prosemirror-tables stores on a cell. `colwidth` holds
 *  one entry per spanned column, and is null until someone drags. */
export interface CellWidthAttrs {
  colspan: number
  colwidth: number[] | null
}

export interface ColumnSizing {
  /** Explicit width per column in px, or null for one nobody has dragged. */
  widths: (number | null)[]
  /** Inline `width` for the <table>, or "" when it should not be fixed. */
  tableWidth: string
  /** Inline `min-width` for the <table>, or "" when the width is fixed. */
  tableMinWidth: string
}

/**
 * The whole width policy, as arithmetic — no DOM, so the rules that decide
 * whether a table overflows are testable on their own.
 *
 * Two deliberate differences from Tiptap's version. An unsized column
 * contributes the *default* width to the total, which is what lets a wide table
 * outgrow the content column; and an explicit width is passed through rather
 * than clamped up to that default, so a column someone dragged narrow stays
 * narrow. The drag floor already bounds how small it can get.
 */
export function computeColumnSizing(cells: readonly CellWidthAttrs[]): ColumnSizing {
  const widths: (number | null)[] = []
  let totalWidth = 0
  let everyColumnSized = true

  for (const { colspan, colwidth } of cells) {
    for (let j = 0; j < colspan; j += 1) {
      const width = colwidth?.[j]
      // A zero entry is prosemirror-tables' "not set" filler, so it counts as
      // unsized rather than as a zero-width column.
      const sized = typeof width === "number" && width > 0 ? width : null
      widths.push(sized)
      totalWidth += sized ?? WIKI_COLUMN_DEFAULT_WIDTH
      if (sized === null) everyColumnSized = false
    }
  }

  // A table with no columns would otherwise come out `width: 0px`, since
  // "every column is sized" is vacuously true. Size nothing instead.
  if (widths.length === 0) return { widths, tableWidth: "", tableMinWidth: "" }

  // Once every column has a width the total *is* the width; until then it is a
  // floor, so the table still stretches to fill a wider content column.
  return {
    widths,
    tableWidth: everyColumnSized ? `${totalWidth}px` : "",
    tableMinWidth: everyColumnSized ? "" : `${totalWidth}px`,
  }
}

/** First-row cell attrs, which are what carry the column widths. */
function rowCells(node: ProseMirrorNode): CellWidthAttrs[] {
  const row = node.firstChild
  if (!row) return []
  return Array.from({ length: row.childCount }, (_, i) => row.child(i).attrs as CellWidthAttrs)
}

/** Writes the <colgroup> and the table's own width to match the node. */
function applyColumnWidths(
  node: ProseMirrorNode,
  colgroup: HTMLElement,
  table: HTMLElement,
): void {
  const { widths, tableWidth, tableMinWidth } = computeColumnSizing(rowCells(node))
  let nextCol = colgroup.firstChild as HTMLElement | null

  for (const width of widths) {
    // An unsized column carries no width at all: `table-layout: fixed` then
    // splits what the table's min-width bought between the unsized columns,
    // which is how each one lands on the default.
    const css = width === null ? "" : `${width}px`
    if (!nextCol) {
      const col = document.createElement("col")
      col.style.width = css
      colgroup.appendChild(col)
    } else {
      if (nextCol.style.width !== css) nextCol.style.width = css
      nextCol = nextCol.nextSibling as HTMLElement | null
    }
  }

  // Columns removed since the last render leave <col> elements behind.
  while (nextCol) {
    const after = nextCol.nextSibling as HTMLElement | null
    nextCol.parentNode?.removeChild(nextCol)
    nextCol = after
  }

  table.style.width = tableWidth
  table.style.minWidth = tableMinWidth
}

/**
 * Node view for the table. Used in both directions: Tiptap instantiates it for
 * read-only documents, and columnResizing instantiates it while editing.
 *
 * The `cellMinWidth` argument the caller passes is ignored on purpose. It is
 * whichever of the two numbers that particular call site happens to hold, and
 * the whole point here is that one number cannot serve both roles.
 */
export class WikiTableView implements NodeView {
  dom: HTMLElement
  contentDOM: HTMLElement
  private table: HTMLTableElement
  private colgroup: HTMLElement
  private node: ProseMirrorNode

  constructor(
    node: ProseMirrorNode,
    _cellMinWidth: number,
    _view: unknown,
    HTMLAttributes: Record<string, unknown> = {},
  ) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "tableWrapper"
    this.table = this.dom.appendChild(document.createElement("table"))
    for (const [key, value] of Object.entries(HTMLAttributes)) {
      if (value !== undefined && value !== null) {
        this.table.setAttribute(key, String(value))
      }
    }
    this.colgroup = this.table.appendChild(document.createElement("colgroup"))
    applyColumnWidths(node, this.colgroup, this.table)
    this.contentDOM = this.table.appendChild(document.createElement("tbody"))
  }

  update(node: ProseMirrorNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    applyColumnWidths(node, this.colgroup, this.table)
    return true
  }

  ignoreMutation(record: ViewMutationRecord): boolean {
    // The width attributes this view writes are its own doing, not user edits.
    return (
      record.type === "attributes" &&
      (record.target === this.table || this.colgroup.contains(record.target))
    )
  }
}

/** The <colgroup> for the static render path — getHTML(), clipboard, export.
 *  Same arithmetic as the node view, expressed as a DOM output spec. */
function staticColGroup(node: ProseMirrorNode) {
  const { widths, tableWidth, tableMinWidth } = computeColumnSizing(rowCells(node))
  const cols = widths.map((width) =>
    width === null ? ["col", {}] : ["col", { style: `width: ${width}px` }],
  )
  return { colgroup: ["colgroup", {}, ...cols], tableWidth, tableMinWidth }
}

export const WikiTable = Table.extend({
  addOptions(): TableOptions {
    return {
      ...(this.parent?.() as TableOptions),
      // Read by the base extension wherever it means "the drag floor"; the
      // default width is not routed through options at all, so there is no way
      // to set one of the two and silently move the other.
      cellMinWidth: WIKI_COLUMN_MIN_WIDTH,
      View: WikiTableView,
    }
  },

  renderHTML({ node, HTMLAttributes }) {
    const { colgroup, tableWidth, tableMinWidth } = staticColGroup(node)
    const existingStyle = HTMLAttributes.style as string | undefined
    const style =
      existingStyle ?? (tableWidth ? `width: ${tableWidth}` : `min-width: ${tableMinWidth}`)

    return [
      "table",
      mergeAttributes(this.options.HTMLAttributes, HTMLAttributes, { style }),
      colgroup,
      ["tbody", 0],
    ]
  },

  addProseMirrorPlugins() {
    return [
      ...(this.options.resizable && this.editor.isEditable
        ? [
            columnResizing({
              handleWidth: this.options.handleWidth,
              // The floor a drag may reach...
              cellMinWidth: this.options.cellMinWidth,
              // ...and the width an undragged column is assumed to have. This
              // one also drives the live preview during a drag, so it has to
              // match what WikiTableView settles on afterwards or every column
              // jumps on mouseup.
              defaultCellMinWidth: WIKI_COLUMN_DEFAULT_WIDTH,
              View: this.options.View ?? WikiTableView,
              lastColumnResizable: this.options.lastColumnResizable,
            }),
          ]
        : []),
      tableEditing({ allowTableNodeSelection: this.options.allowTableNodeSelection }),
    ]
  },
})

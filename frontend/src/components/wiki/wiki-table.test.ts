import { describe, expect, it } from "vitest"
import {
  computeColumnSizing,
  WIKI_COLUMN_DEFAULT_WIDTH,
  WIKI_COLUMN_MIN_WIDTH,
  type CellWidthAttrs,
} from "./wiki-table"

/** N columns nobody has ever dragged — what every freshly inserted table is. */
function unsized(count: number): CellWidthAttrs[] {
  return Array.from({ length: count }, () => ({ colspan: 1, colwidth: null }))
}

describe("computeColumnSizing", () => {
  // The production bug this guards. Tiptap's stock cellMinWidth of 25 made a
  // 20-column table ask for 500px; the `width: 100%` in wiki-editor.css beat
  // that at any realistic content width, so the columns divided the content
  // column evenly, collapsed to roughly one letter each, and .tableWrapper had
  // no overflow to scroll. The table has to out-demand the content column.
  it("asks for more than any content column once a table is wide", () => {
    expect(computeColumnSizing(unsized(20)).tableMinWidth).toBe("2400px")
    expect(computeColumnSizing(unsized(12)).tableMinWidth).toBe("1440px")
  })

  it("leaves a narrow table free to stretch to the content column", () => {
    // 480px is under any realistic content width, so `width: 100%` still wins
    // and a four-column table fills the page as it always did.
    const sizing = computeColumnSizing(unsized(4))
    expect(sizing.tableMinWidth).toBe("480px")
    expect(sizing.tableWidth).toBe("")
  })

  it("does not fix the width while any column is undragged", () => {
    const sizing = computeColumnSizing([
      { colspan: 1, colwidth: [300] },
      { colspan: 1, colwidth: null },
    ])
    expect(sizing.tableWidth).toBe("")
    expect(sizing.tableMinWidth).toBe(`${300 + WIKI_COLUMN_DEFAULT_WIDTH}px`)
    expect(sizing.widths).toEqual([300, null])
  })

  it("fixes the width once every column has been dragged", () => {
    const sizing = computeColumnSizing([
      { colspan: 1, colwidth: [300] },
      { colspan: 1, colwidth: [150] },
    ])
    expect(sizing.tableWidth).toBe("450px")
    expect(sizing.tableMinWidth).toBe("")
  })

  // The reason the default and the floor are separate numbers at all. Tiptap
  // spends one option on both, so raising the default to make wide tables
  // scroll would also clamp a deliberately narrow column back up to it.
  it("keeps a column dragged below the default at the width it was given", () => {
    const narrow = WIKI_COLUMN_MIN_WIDTH + 5
    expect(narrow).toBeLessThan(WIKI_COLUMN_DEFAULT_WIDTH)

    const sizing = computeColumnSizing([{ colspan: 1, colwidth: [narrow] }])
    expect(sizing.widths).toEqual([narrow])
    expect(sizing.tableWidth).toBe(`${narrow}px`)
  })

  it("counts a merged cell once per column it spans", () => {
    const sizing = computeColumnSizing([
      { colspan: 3, colwidth: [100, 200, 300] },
      { colspan: 1, colwidth: [50] },
    ])
    expect(sizing.widths).toEqual([100, 200, 300, 50])
    expect(sizing.tableWidth).toBe("650px")
  })

  it("treats a merged cell's unfilled slots as undragged", () => {
    // prosemirror-tables pads colwidth with zeroes for spans it has no width
    // for; a zero-width column would collapse the whole table.
    const sizing = computeColumnSizing([{ colspan: 3, colwidth: [120, 0, 0] }])
    expect(sizing.widths).toEqual([120, null, null])
    expect(sizing.tableMinWidth).toBe(`${120 + WIKI_COLUMN_DEFAULT_WIDTH * 2}px`)
  })

  it("sizes nothing for a table with no columns", () => {
    // "every column is sized" is vacuously true here, which would otherwise
    // pin the table to width: 0px.
    expect(computeColumnSizing([])).toEqual({
      widths: [],
      tableWidth: "",
      tableMinWidth: "",
    })
  })
})

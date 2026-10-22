// XLSX → HTML conversion for the attachment preview panel.
//
// Unlike the docx path, nothing here renders markup that came out of the
// document. read-excel-file hands back plain cell *values*; we build the table
// ourselves and escape every value on the way in. That makes this the safest
// renderer in the panel — there is no attacker-authored markup at any point,
// only text placed into a structure we control.
//
// It also sidesteps the SheetJS trap. npm's `xlsx` is pinned at 0.18.5
// (published 2022-03-24) and carries two unfixed high-severity advisories —
// GHSA-4r6h-8v6p-xvw6 (prototype pollution) and GHSA-5pgg-2g8v-p4x9 (ReDoS) —
// because the maintainers publish fixes only to their own CDN. Feeding hostile
// spreadsheets to a known-vulnerable parser is not a trade worth making on this
// platform. read-excel-file is MIT, actively published, audits clean, and is
// ~13 kB gzipped against SheetJS's ~308 kB.
//
// Number formats are kept: a value is shown as Excel shows it ($84,219, 12%,
// 2024-03-01), read back by wiki-file-preview-xlsx-formats.ts. What is lost
// versus a full spreadsheet component: fonts, colours, merged-cell geometry,
// charts, and formulas (cached results are shown, not the formula text). For a
// preview that is an acceptable trade; anyone needing the real thing downloads
// the file.

import {
  escapeHtml,
  noteBody,
  type PreviewBody,
} from "./wiki-file-preview-document"
import { readCellFormats } from "./wiki-file-preview-xlsx-formats"

/** Caps that keep a hostile or merely enormous workbook from locking up the
 *  operator's tab. A spreadsheet claiming a million rows is a denial-of-service
 *  vector against the person opening it, and this panel lives inside a
 *  collaboratively-edited wiki page — a frozen tab costs unsaved work, not just
 *  a reload. Exceeding any cap is reported in the document, never silently. */
const MAX_SHEETS = 25
const MAX_ROWS_PER_SHEET = 500
const MAX_COLUMNS = 60

/** Mirrors read-excel-file's Row type without importing it, so this module's
 *  pure half stays testable without pulling the library in. Values arrive from
 *  an untrusted file, so they are narrowed rather than asserted. */
type CellInput = unknown
type RowInput = readonly CellInput[]

export interface SheetInput {
  /** Sheet name, shown as a heading above the table. Pass "" to omit the
   *  heading — used by the CSV renderer, where there is only ever one table and
   *  a title would just repeat the filename already shown on the card. */
  sheet: string
  data: readonly RowInput[]
}

export interface SheetsHtmlResult {
  bodyHtml: string
  /** Human-readable summary of what was dropped, or undefined when nothing was. */
  notice?: string
}

/** Spreadsheet column label for a zero-based index: 0 → A, 25 → Z, 26 → AA. */
export function columnLabel(index: number): string {
  let label = ""
  let n = index
  while (n >= 0) {
    label = String.fromCharCode(65 + (n % 26)) + label
    n = Math.floor(n / 26) - 1
  }
  return label
}

/** A value already rendered through its cell's number format, by
 *  renderXlsxPreview. The parser yields only primitives and Dates, so a cell
 *  of this shape can only have come from there. */
export class FormattedCell {
  readonly text: string
  readonly numeric: boolean
  constructor(text: string, numeric: boolean) {
    this.text = text
    this.numeric = numeric
  }
}

/** Renders one cell value as display text. Returns the text plus whether it
 *  should be right-aligned, which is how a spreadsheet signals "this is a
 *  number" without us having to restyle per cell. */
function formatCell(value: CellInput): { text: string; numeric: boolean } {
  if (value instanceof FormattedCell) return { text: value.text, numeric: value.numeric }
  if (value === null || value === undefined) return { text: "", numeric: false }
  if (typeof value === "number") {
    return { text: Number.isFinite(value) ? String(value) : "", numeric: true }
  }
  if (typeof value === "boolean") return { text: value ? "TRUE" : "FALSE", numeric: false }
  if (value instanceof Date) {
    // Trim the time component when it carries no information — date-only cells
    // are the common case and "2024-03-01" reads better than a full timestamp.
    const iso = value.toISOString()
    const text = iso.endsWith("T00:00:00.000Z") ? iso.slice(0, 10) : iso.replace(".000Z", "Z")
    return { text, numeric: false }
  }
  return { text: String(value), numeric: false }
}

/** True when a row carries no content at all — trailing empties are extremely
 *  common in exported sheets and rendering them wastes the whole panel. */
function isBlankRow(row: RowInput): boolean {
  return row.every((cell) => formatCell(cell).text === "")
}

/** Drops trailing blank rows, then trailing blank columns, so the rendered
 *  table matches the sheet's actual used range rather than its declared one. */
function trimSheet(data: readonly RowInput[]): RowInput[] {
  const rows = [...data]
  while (rows.length > 0 && isBlankRow(rows[rows.length - 1])) rows.pop()

  let width = 0
  for (const row of rows) {
    for (let i = row.length - 1; i >= 0; i--) {
      if (formatCell(row[i]).text !== "") {
        width = Math.max(width, i + 1)
        break
      }
    }
  }
  return rows.map((row) => row.slice(0, width))
}

/**
 * Lays the sheets out behind a row of tabs along the bottom, as a spreadsheet
 * does, one sheet showing at a time.
 *
 * The frame is sandboxed without scripts, so the switching is HTML and CSS
 * alone: a hidden radio button per sheet, each tab the label of one, and a
 * rule per sheet that shows its panel and marks its tab while its radio is
 * checked. Arrow keys move between them the way they do in any radio group.
 * The radios come first because a CSS rule can only look at what follows.
 */
function sheetTabs(sheets: readonly SheetInput[], panels: readonly string[]): string {
  const radios = sheets
    .map((_, i) => `<input type="radio" name="sheet" id="sheet-${i}" class="sheet-radio"${i === 0 ? " checked" : ""}>`)
    .join("")
  const rules = sheets
    .map(
      (_, i) =>
        `#sheet-${i}:checked ~ .sheet-panels > [data-sheet="${i}"] { display: block; }` +
        `#sheet-${i}:checked ~ .sheet-tabs > [data-sheet="${i}"] { background: var(--tab-active-bg); color: var(--tab-active-fg); border-color: var(--tab-border); }` +
        `#sheet-${i}:focus-visible ~ .sheet-tabs > [data-sheet="${i}"] { outline: 2px solid var(--tab-focus); outline-offset: -3px; }`,
    )
    .join("\n")
  const panelHtml = panels.map((panel, i) => `<section class="sheet-panel" data-sheet="${i}">${panel}</section>`).join("")
  const tabs = sheets
    .map(
      (sheet, i) => {
        // A long name is cut short on its tab; the tooltip has all of it.
        const name = escapeHtml(sheet.sheet || `Sheet${i + 1}`)
        return `<label for="sheet-${i}" class="sheet-tab" data-sheet="${i}" title="${name}">${name}</label>`
      },
    )
    .join("")
  return (
    `<style>${rules}</style>${radios}` +
    `<div class="sheet-panels">${panelHtml}</div>` +
    `<nav class="sheet-tabs" aria-label="Sheets">${tabs}</nav>`
  )
}

/**
 * Builds the body markup for a whole workbook. Pure — no library, no DOM — so
 * the truncation and escaping rules are unit-testable on their own.
 */
export function buildSheetsHtml(sheets: readonly SheetInput[]): SheetsHtmlResult {
  const dropped: string[] = []

  const visible = sheets.slice(0, MAX_SHEETS)
  if (sheets.length > visible.length) {
    dropped.push(`${sheets.length - visible.length} of ${sheets.length} sheets not shown`)
  }

  const panels: string[] = []
  let rowsTruncated = 0
  let colsTruncated = 0

  for (const sheet of visible) {
    const trimmed = trimSheet(sheet.data)
    if (trimmed.length === 0) {
      panels.push('<p class="doc-note">Empty sheet.</p>')
      continue
    }

    const rows = trimmed.slice(0, MAX_ROWS_PER_SHEET)
    if (trimmed.length > rows.length) rowsTruncated += trimmed.length - rows.length

    const fullWidth = rows.reduce((max, row) => Math.max(max, row.length), 0)
    const width = Math.min(fullWidth, MAX_COLUMNS)
    if (fullWidth > width) colsTruncated = Math.max(colsTruncated, fullWidth - width)

    const header = Array.from(
      { length: width },
      (_, i) => `<th>${columnLabel(i)}</th>`,
    ).join("")

    const body = rows
      .map((row, rowIndex) => {
        const cells = Array.from({ length: width }, (_, colIndex) => {
          const { text, numeric } = formatCell(row[colIndex])
          return `<td${numeric ? ' class="num"' : ""}>${escapeHtml(text)}</td>`
        }).join("")
        return `<tr><th>${rowIndex + 1}</th>${cells}</tr>`
      })
      .join("")

    panels.push(
      `<div class="doc-scroll"><table><thead><tr><th></th>${header}</tr></thead>` +
        `<tbody>${body}</tbody></table></div>`,
    )
  }

  if (rowsTruncated > 0) dropped.push(`${rowsTruncated} rows beyond the first ${MAX_ROWS_PER_SHEET} per sheet`)
  if (colsTruncated > 0) dropped.push(`columns beyond the first ${MAX_COLUMNS}`)

  return {
    // A CSV is one sheet without a name: the filename is already on the card,
    // so it gets no tab.
    bodyHtml:
      visible.length === 0
        ? ""
        : visible.length === 1 && visible[0].sheet === ""
          ? panels.join("")
          : sheetTabs(visible, panels),
    notice:
      dropped.length === 0
        ? undefined
        : `Preview truncated — ${dropped.join("; ")}. Download the file to see everything.`,
  }
}

/** Milliseconds from the Unix epoch to Excel's day zero (1899-12-30). */
const EXCEL_EPOCH_MS = Date.UTC(1899, 11, 30)

/**
 * Renders every number and date through its cell's format, as Excel would
 * show it. A cell with no format gets General, which is also what keeps a
 * float from printing all seventeen digits. Pure apart from the two
 * functions passed in, so it is testable without the libraries.
 */
export function applyNumberFormats(
  data: readonly RowInput[],
  formatAt: ((row: number, column: number) => string | null) | undefined,
  display: (pattern: string, value: number) => string | null,
): RowInput[] {
  return data.map((row, r) =>
    row.map((value, c) => {
      // Past the preview's caps the row is cut anyway (and counted in the
      // truncation notice), and no format was read for it.
      if (r >= MAX_ROWS_PER_SHEET || c >= MAX_COLUMNS) return value
      const pattern = formatAt?.(r, c) ?? null
      if (typeof value === "number" && Number.isFinite(value)) {
        const text = display(pattern ?? "General", value)
        return text === null ? value : new FormattedCell(text, true)
      }
      if (value instanceof Date && Number.isFinite(value.getTime())) {
        // read-excel-file builds the Date in UTC; the formatter reads a
        // Date in local time, so hand it the serial Excel stored instead.
        const serial = (value.getTime() - EXCEL_EPOCH_MS) / 86_400_000
        const text = pattern === null ? null : display(pattern, serial)
        return text === null ? value : new FormattedCell(text, true)
      }
      return value
    }),
  )
}

/**
 * Parses xlsx bytes into preview content.
 *
 * Throws on a payload read-excel-file cannot parse; the caller renders that as
 * the panel's error state.
 */
export async function renderXlsxPreview(bytes: ArrayBuffer): Promise<PreviewBody> {
  // Subpath import is mandatory: the package declares an `exports` map with no
  // "." entry, so the bare specifier does not resolve under bundler resolution.
  const { default: readXlsxFile } = await import("read-excel-file/browser")

  const [sheets, { format }] = await Promise.all([readXlsxFile(bytes), import("numfmt")])
  const formats = readCellFormats(bytes, MAX_ROWS_PER_SHEET, MAX_COLUMNS)
  const display = (pattern: string, value: number): string | null => {
    try {
      return format(pattern, value)
    } catch {
      return null
    }
  }
  const { bodyHtml, notice } = buildSheetsHtml(
    sheets.map(({ sheet, data }, i) => {
      // By name, falling back to position: both come from workbook.xml.
      const sheetFormats = formats.find((f) => f.name === sheet) ?? formats[i]
      return { sheet, data: applyNumberFormats(data, sheetFormats?.formatAt, display) }
    }),
  )

  return bodyHtml === ""
    ? noteBody("This workbook has no sheets.")
    : { bodyHtml, notice }
}

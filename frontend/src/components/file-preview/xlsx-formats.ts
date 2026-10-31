// Each cell's number format, for the xlsx preview.
//
// read-excel-file hands back values only: it reads the number formats to tell
// a date from a number, then drops them. Without them a currency cell shows
// as 84219.49731068664 where Excel shows $84,219, so this reads them back out
// of the same file: styles.xml maps a cell's style index to a format, and the
// sheet XML gives each cell its style index.
//
// Only attribute values are read, out of a file we never render as markup.
// Anything malformed yields no formats rather than an error, and the preview
// falls back to Excel's General format, which is what a cell without one gets
// anyway.

import { unzipSync } from "fflate"

/** Excel's built-in formats, by id. A file names only the formats it
 *  defines itself; these it may use without defining. 14 and 22 are the
 *  locale's short date: written here as ISO, which no reader misreads. */
const BUILT_IN_FORMATS: Record<number, string> = {
  0: "General",
  1: "0",
  2: "0.00",
  3: "#,##0",
  4: "#,##0.00",
  9: "0%",
  10: "0.00%",
  11: "0.00E+00",
  12: "# ?/?",
  13: "# ??/??",
  14: "yyyy-mm-dd",
  15: "d-mmm-yy",
  16: "d-mmm",
  17: "mmm-yy",
  18: "h:mm AM/PM",
  19: "h:mm:ss AM/PM",
  20: "h:mm",
  21: "h:mm:ss",
  22: "yyyy-mm-dd h:mm",
  37: "#,##0 ;(#,##0)",
  38: "#,##0 ;[Red](#,##0)",
  39: "#,##0.00;(#,##0.00)",
  40: "#,##0.00;[Red](#,##0.00)",
  45: "mm:ss",
  46: "[h]:mm:ss",
  47: "mmss.0",
  48: "##0.0E+0",
  49: "@",
}

export interface SheetFormats {
  /** The sheet's name, as read-excel-file reports it. */
  name: string
  /** The format code of a cell, by zero-based row and column; null when the
   *  cell has none beyond General. */
  formatAt(row: number, column: number): string | null
}

/**
 * The number formats of every sheet in the workbook, in workbook order.
 * Reads cells only up to maxRows and maxColumns, the most the preview shows.
 */
export function readCellFormats(bytes: ArrayBuffer, maxRows: number, maxColumns: number): SheetFormats[] {
  try {
    const files = unzipSync(new Uint8Array(bytes), {
      filter: (file) => (file.name.startsWith("xl/") && file.name.endsWith(".xml")) || file.name.endsWith(".rels"),
    })
    const text = (name: string) => {
      const data = files[name]
      return data ? new TextDecoder().decode(data) : ""
    }

    const styleFormats = parseStyleFormats(text("xl/styles.xml"))
    const targets = parseRelationships(text("xl/_rels/workbook.xml.rels"))
    return parseSheetList(text("xl/workbook.xml")).map(({ name, relationshipId }) => {
      const target = targets.get(relationshipId)
      const xml = target ? text(resolveTarget(target)) : ""
      const cells = parseCellStyles(xml, maxRows, maxColumns)
      return {
        name,
        formatAt: (row, column) => {
          const style = cells.get(row * maxColumns + column)
          const format = style === undefined ? undefined : styleFormats[style]
          return format === undefined || format === "General" ? null : format
        },
      }
    })
  } catch {
    return []
  }
}

/** Format code per cell style index (cellXfs order). */
export function parseStyleFormats(stylesXml: string): string[] {
  const custom = new Map<number, string>()
  for (const tag of tags(section(stylesXml, "numFmts"), "numFmt")) {
    const id = Number(tag.numFmtId)
    if (Number.isInteger(id) && tag.formatCode !== undefined) custom.set(id, tag.formatCode)
  }
  // cellXfs, not cellStyleXfs: both hold <xf> elements, and only the first
  // is what a cell's s attribute indexes.
  return tags(section(stylesXml, "cellXfs"), "xf").map((xf) => {
    const id = Number(xf.numFmtId ?? 0)
    return custom.get(id) ?? BUILT_IN_FORMATS[id] ?? "General"
  })
}

function parseSheetList(workbookXml: string): { name: string; relationshipId: string }[] {
  return tags(section(workbookXml, "sheets"), "sheet")
    .map((sheet) => ({ name: sheet.name ?? "", relationshipId: sheet["r:id"] ?? "" }))
    .filter((sheet) => sheet.relationshipId !== "")
}

function parseRelationships(relsXml: string): Map<string, string> {
  const targets = new Map<string, string>()
  for (const rel of tags(relsXml, "Relationship")) {
    if (rel.Id && rel.Target) targets.set(rel.Id, rel.Target)
  }
  return targets
}

/** A workbook relationship's target as a path in the zip: relative to xl/
 *  unless it starts at the root. */
function resolveTarget(target: string): string {
  return target.startsWith("/") ? target.slice(1) : `xl/${target}`
}

/** Style index of each styled cell, keyed by row * maxColumns + column. */
export function parseCellStyles(sheetXml: string, maxRows: number, maxColumns: number): Map<number, number> {
  const styles = new Map<number, number>()
  const cell = /<c\b([^>]*)>/g
  let match: RegExpExecArray | null
  while ((match = cell.exec(sheetXml)) !== null) {
    const attrs = attributes(match[1])
    if (attrs.s === undefined || attrs.r === undefined) continue
    const address = parseAddress(attrs.r)
    if (!address) continue
    // Cells come in row order, so the first one past the cap ends the scan.
    if (address.row >= maxRows) break
    if (address.column >= maxColumns) continue
    const style = Number(attrs.s)
    if (Number.isInteger(style)) styles.set(address.row * maxColumns + address.column, style)
  }
  return styles
}

/** "E12" → row 11, column 4 (zero-based). */
export function parseAddress(ref: string): { row: number; column: number } | null {
  const m = /^([A-Z]{1,3})(\d{1,7})$/.exec(ref)
  if (!m) return null
  let column = 0
  for (const ch of m[1]) column = column * 26 + (ch.charCodeAt(0) - 64)
  return { row: Number(m[2]) - 1, column: column - 1 }
}

/** The inside of the first <name>…</name>, or "" when there is none. */
function section(xml: string, name: string): string {
  const open = new RegExp(`<${name}\\b[^>]*>`).exec(xml)
  if (!open) return ""
  const start = open.index + open[0].length
  const end = xml.indexOf(`</${name}>`, start)
  return end === -1 ? "" : xml.slice(start, end)
}

/** The attributes of every <name …> element in xml. */
function tags(xml: string, name: string): Record<string, string>[] {
  const out: Record<string, string>[] = []
  const pattern = new RegExp(`<${name}\\b([^>]*)>`, "g")
  let match: RegExpExecArray | null
  while ((match = pattern.exec(xml)) !== null) out.push(attributes(match[1]))
  return out
}

function attributes(source: string): Record<string, string> {
  const out: Record<string, string> = {}
  const pattern = /([\w:]+)\s*=\s*(?:"([^"]*)"|'([^']*)')/g
  let match: RegExpExecArray | null
  while ((match = pattern.exec(source)) !== null) out[match[1]] = decodeEntities(match[2] ?? match[3] ?? "")
  return out
}

function decodeEntities(value: string): string {
  return value.replace(/&(#x[0-9a-fA-F]+|#\d+|quot|apos|lt|gt|amp);/g, (_, entity: string) => {
    if (entity[0] === "#") {
      const code = entity[1] === "x" ? parseInt(entity.slice(2), 16) : parseInt(entity.slice(1), 10)
      return Number.isFinite(code) && code <= 0x10ffff ? String.fromCodePoint(code) : ""
    }
    return { quot: '"', apos: "'", lt: "<", gt: ">", amp: "&" }[entity] ?? ""
  })
}

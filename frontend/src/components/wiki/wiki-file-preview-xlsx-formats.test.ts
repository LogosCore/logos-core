import { zipSync, strToU8 } from "fflate"
import { describe, expect, it } from "vitest"

import { parseAddress, parseStyleFormats, readCellFormats } from "./wiki-file-preview-xlsx-formats"
import { renderXlsxPreview } from "./wiki-file-preview-xlsx"

const CONTENT_TYPES = `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`

// Style 1 is a custom currency format, 2 a built-in percentage, 3 a
// built-in date. cellStyleXfs comes first and must not be mistaken for them.
const STYLES = `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="&quot;$&quot;#,##0"/></numFmts><cellStyleXfs count="1"><xf numFmtId="10"/></cellStyleXfs><cellXfs count="4"><xf numFmtId="0"/><xf numFmtId="164" applyNumberFormat="1"/><xf numFmtId="9" applyNumberFormat="1"/><xf numFmtId="14" applyNumberFormat="1"/></cellXfs></styleSheet>`

function workbook(sheetData: string): ArrayBuffer {
  const files = {
    "[Content_Types].xml": strToU8(CONTENT_TYPES),
    "_rels/.rels": strToU8(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`),
    "xl/workbook.xml": strToU8(`<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sales &amp; costs" sheetId="1" r:id="rId1"/></sheets></workbook>`),
    "xl/_rels/workbook.xml.rels": strToU8(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`),
    "xl/styles.xml": strToU8(STYLES),
    "xl/worksheets/sheet1.xml": strToU8(`<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>${sheetData}</sheetData></worksheet>`),
  }
  const zipped = zipSync(files)
  return zipped.buffer.slice(zipped.byteOffset, zipped.byteOffset + zipped.byteLength) as ArrayBuffer
}

describe("parseStyleFormats", () => {
  it("resolves custom and built-in formats in cellXfs order", () => {
    expect(parseStyleFormats(STYLES)).toEqual(["General", '"$"#,##0', "0%", "yyyy-mm-dd"])
  })

  it("is empty for a workbook without styles", () => {
    expect(parseStyleFormats("")).toEqual([])
  })
})

describe("parseAddress", () => {
  it("reads multi-letter columns", () => {
    expect(parseAddress("E12")).toEqual({ row: 11, column: 4 })
    expect(parseAddress("AA1")).toEqual({ row: 0, column: 26 })
    expect(parseAddress("e12")).toBeNull()
  })
})

describe("readCellFormats", () => {
  it("maps each styled cell to its format and leaves General out", () => {
    const [sheet] = readCellFormats(workbook(`<row r="1"><c r="A1" s="1"><v>5</v></c><c r="B1" s="0"><v>6</v></c><c r="C1" s="2"><v>0.5</v></c></row>`), 500, 60)
    expect(sheet.name).toBe("Sales & costs")
    expect(sheet.formatAt(0, 0)).toBe('"$"#,##0')
    expect(sheet.formatAt(0, 1)).toBeNull()
    expect(sheet.formatAt(0, 2)).toBe("0%")
    expect(sheet.formatAt(5, 5)).toBeNull()
  })

  it("is empty, not an error, for bytes that are not a workbook", () => {
    expect(readCellFormats(new TextEncoder().encode("not a zip").buffer as ArrayBuffer, 500, 60)).toEqual([])
  })
})

describe("renderXlsxPreview", () => {
  it("shows values as Excel formats them", async () => {
    // Row 1 is empty: the formats must still land on the right rows.
    const { bodyHtml } = await renderXlsxPreview(
      workbook(
        `<row r="2"><c r="A2" s="1"><v>84219.497310686638</v></c><c r="B2" s="2"><v>0.125</v></c><c r="C2" s="3"><v>45352</v></c><c r="D2"><v>0.30000000000000004</v></c><c r="E2"><v>2011</v></c></row>`,
      ),
    )
    expect(bodyHtml).toContain('<td class="num">$84,219</td>')
    expect(bodyHtml).toContain('<td class="num">13%</td>')
    expect(bodyHtml).toContain('<td class="num">2024-03-01</td>')
    // General: no float noise, and integers stay as they are.
    expect(bodyHtml).toContain('<td class="num">0.3</td>')
    expect(bodyHtml).toContain('<td class="num">2011</td>')
    expect(bodyHtml).not.toContain("84219.49")
  })
})

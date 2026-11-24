import { describe, expect, it } from "vitest"
import { encodingName, firstLine, groupDecodings, orderDecodings, visibleText, type OutputDecoding } from "./output-encoding-values"

const rows: OutputDecoding[] = [
  { encoding: "utf-8", name: "UTF-8", text: "\uFFFD\uFFFD", unreadable: 2 },
  { encoding: "utf-16le", name: "UTF-16LE", text: "\u4e00", unreadable: 1 },
  { encoding: "ibm437", name: "OEM United States (437)", text: "Å", unreadable: 0 },
  { encoding: "ibm866", name: "OEM Russian (866)", text: "Привет", unreadable: 0 },
]

describe("orderDecodings", () => {
  it("puts the fewest unreadable first, core's order among equals", () => {
    expect(orderDecodings(rows, "").map((d) => d.encoding)).toEqual(["ibm437", "ibm866", "utf-16le", "utf-8"])
  })
  it("filters by name or id", () => {
    expect(orderDecodings(rows, " Russian ").map((d) => d.encoding)).toEqual(["ibm866"])
    expect(orderDecodings(rows, "866").map((d) => d.encoding)).toEqual(["ibm866"])
    expect(orderDecodings(rows, "utf").map((d) => d.encoding)).toEqual(["utf-16le", "utf-8"])
  })
})

describe("groupDecodings", () => {
  const ascii: OutputDecoding[] = [
    { encoding: "utf-8", name: "UTF-8", text: "uid=0", unreadable: 0 },
    { encoding: "utf-16le", name: "UTF-16LE", text: "\u6975", unreadable: 3 },
    { encoding: "ibm437", name: "OEM United States (437)", text: "uid=0", unreadable: 0 },
    { encoding: "ibm866", name: "OEM Russian (866)", text: "uid=0", unreadable: 0 },
  ]
  it("makes one row of those that read alike, the first standing for it", () => {
    const g = groupDecodings(ascii, "", "utf-8")
    expect(g.map((r) => [r.encoding, r.members.length])).toEqual([["utf-8", 3], ["utf-16le", 1]])
  })
  it("lets the session's encoding stand for its row", () => {
    const g = groupDecodings(ascii, "", "ibm866")
    expect(g[0].encoding).toBe("ibm866")
    expect(g[0].members.map((d) => d.encoding)).toEqual(["ibm866", "utf-8", "ibm437"])
  })
  it("groups what the filter keeps", () => {
    expect(groupDecodings(ascii, "oem", "utf-8").map((r) => [r.encoding, r.members.length])).toEqual([["ibm437", 2]])
  })
})

describe("visibleText", () => {
  it("shows control characters and drops a CR before a LF", () => {
    expect(visibleText("a\u0000b\r\nc\td\re\u007f\u0085")).toBe("a\u2400b\nc\td\u240de\u2421\u00b7")
  })
})

describe("firstLine", () => {
  it("is the first line with something on it", () => {
    expect(firstLine("\r\n  \r\nC:\\Привет\r\nnext")).toBe("C:\\Привет")
    expect(firstLine("")).toBe("")
  })
})

describe("encodingName", () => {
  it("names the encoding, UTF-8 for none", () => {
    expect(encodingName(rows, "ibm866")).toBe("OEM Russian (866)")
    expect(encodingName(rows, null)).toBe("UTF-8")
    expect(encodingName([], "koi8-r")).toBe("KOI8-R")
  })
})

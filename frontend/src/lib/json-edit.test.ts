import { describe, expect, it } from "vitest"
import { formatJson, indentJsonText } from "./json-edit"

/** text with the whitespace between its tokens taken out, strings untouched. */
function compact(text: string): string {
  let out = ""
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (c === '"') {
      let j = i + 1
      while (text[j] !== '"') j += text[j] === "\\" ? 2 : 1
      out += text.slice(i, j + 1)
      i = j
    } else if (!/[ \n\r\t]/.test(c)) {
      out += c
    }
  }
  return out
}

describe("indentJsonText", () => {
  it("indents as formatJson does", () => {
    const value = { a: 1, b: [true, null, "x"], c: { d: {}, e: [], f: [{ g: "h" }] }, "": -0.5 }
    expect(indentJsonText(JSON.stringify(value))).toBe(formatJson(value))
  })

  it("keeps every token as it was sent: escapes, number forms, key order", () => {
    const sent =
      '{"z":1,"url":"/a?expires=1\\u0026sig=f","quote":"say \\"hi\\" \\\\ ok","n":2.50,"big":9007199254740993,' +
      '"e":1e+21,"a":[ ],"o":{ }}'
    const shown = indentJsonText(sent)
    expect(shown).toContain('"url": "/a?expires=1\\u0026sig=f"')
    expect(shown).toContain('"quote": "say \\"hi\\" \\\\ ok"')
    expect(shown).toContain('"n": 2.50')
    expect(shown).toContain('"big": 9007199254740993')
    expect(shown).toContain('"e": 1e+21')
    expect(shown).toContain('"a": []')
    expect(shown).toContain('"o": {}')
    expect(shown.indexOf('"z"')).toBeLessThan(shown.indexOf('"url"'))
    // Whitespace aside, what is shown is what was sent.
    expect(compact(shown)).toBe(compact(sent))
  })

  it("leaves strings' own spaces, colons, commas and brackets alone", () => {
    expect(indentJsonText('{"k":"a, b: [c] {d}  e"}')).toBe('{\n  "k": "a, b: [c] {d}  e"\n}')
  })

  it("gives back text that is not JSON as it is", () => {
    expect(indentJsonText('{"a":')).toBe('{"a":')
    expect(indentJsonText("")).toBe("")
  })

  it("writes a lone value as it is", () => {
    expect(indentJsonText(' "x" ')).toBe('"x"')
    expect(indentJsonText("12")).toBe("12")
  })
})

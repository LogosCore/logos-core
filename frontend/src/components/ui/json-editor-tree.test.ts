import { describe, expect, it } from "vitest"
import { jsonLanguage } from "@codemirror/lang-json"
import { firstErrorAt, keysOf, pointerOfNode, propertySpans } from "./json-editor-tree"

const parse = (doc: string) => jsonLanguage.parser.parse(doc)
const slice = (doc: string, from: number, to: number) => doc.slice(from, to)

describe("propertySpans", () => {
  const doc = '{"format": "x", "channel": {"module_name": "http"}, "params": {"a/b": [1, true], "n": null}}'
  const spans = propertySpans(parse(doc), doc)

  it("finds each property by its pointer, keys escaped", () => {
    const format = spans.get("/format")!
    expect(slice(doc, format.from, format.to)).toBe('"format": "x"')
    expect(slice(doc, format.keyFrom, format.keyTo)).toBe('"format"')
    expect(slice(doc, format.valueFrom, format.valueTo)).toBe('"x"')
    expect(format.valueType).toBe("String")
    expect(slice(doc, spans.get("/channel/module_name")!.valueFrom, spans.get("/channel/module_name")!.valueTo)).toBe(
      '"http"',
    )
    expect(spans.get("/params/a~1b")!.valueType).toBe("Array")
    expect(spans.get("/params/n")!.valueType).toBe("Null")
  })

  it("finds array items and the document", () => {
    expect(slice(doc, spans.get("/params/a~1b/1")!.from, spans.get("/params/a~1b/1")!.to)).toBe("true")
    expect(spans.get("")!.valueType).toBe("Object")
  })

  it("keeps a key written twice at its first place", () => {
    const twice = '{"a": 1, "a": 2}'
    const a = propertySpans(parse(twice), twice).get("/a")!
    expect(slice(twice, a.valueFrom, a.valueTo)).toBe("1")
  })

  it("places a property whose value is missing at its key", () => {
    const broken = '{"a": 1, "b": }'
    const b = propertySpans(parse(broken), broken).get("/b")!
    expect(b.valueType).toBe("")
    expect(b.valueFrom).toBe(b.keyTo)
  })
})

describe("pointerOfNode", () => {
  it("is an object's or array's place from the document", () => {
    const doc = '{"params": {"list": [{"x": 1}]}}'
    const tree = parse(doc)
    const at = (pos: number) => {
      let n = tree.resolveInner(pos, 1)
      while (n.name !== "Object" && n.name !== "Array" && n.parent) n = n.parent
      return pointerOfNode(n, doc)
    }
    expect(at(1)).toBe("")
    expect(at(doc.indexOf('"list"'))).toBe("/params")
    expect(at(doc.indexOf('"x"'))).toBe("/params/list/0")
  })
})

describe("keysOf", () => {
  it("lists an object's keys", () => {
    const doc = '{"a": 1, "b~": {"c": 2}}'
    const object = parse(doc).topNode.firstChild!
    expect([...keysOf(object, doc)]).toEqual(["a", "b~"])
  })
})

describe("firstErrorAt", () => {
  it("is where the document stops being JSON", () => {
    expect(firstErrorAt(parse('{"a": 1}'))).toBeNull()
    expect(firstErrorAt(parse('{"a": 1, }'))).toBe(9)
    expect(firstErrorAt(parse('{"a": }'))).toBe(6)
  })
})

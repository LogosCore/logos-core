import { describe, expect, it } from "vitest"
import { CompletionContext } from "@codemirror/autocomplete"
import { json } from "@codemirror/lang-json"
import { EditorState, type TransactionSpec } from "@codemirror/state"
import type { JsonHints } from "@/lib/json-edit"
import { completeJson, config, diagnosticsOf, keepLocks, setConfig, spans } from "./json-editor-state"

function stateOf(doc: string, locked: string[] = [], hints: JsonHints = {}) {
  const s = EditorState.create({ doc, extensions: [json(), config, spans, keepLocks] })
  return s.update({ effects: setConfig.of({ locked, hints }) }).state
}

describe("keepLocks", () => {
  const doc = '{\n  "format": "x",\n  "name": "a"\n}'
  const locked = stateOf(doc, ["/format"])
  const after = (spec: TransactionSpec) => locked.update(spec).state.doc.toString()

  it("refuses typing into a locked property, or deleting across one", () => {
    expect(after({ changes: { from: doc.indexOf('"x"') + 1, insert: "y" } })).toBe(doc)
    expect(after({ changes: { from: doc.indexOf('"format"') + 1, insert: "z" } })).toBe(doc)
    expect(after({ changes: { from: 1, to: doc.indexOf('"name"') } })).toBe(doc)
  })

  it("lets the rest be edited, up to the locked property's edges", () => {
    const at = doc.indexOf('"a"') + 1
    expect(after({ changes: { from: at, insert: "b" } })).toBe(doc.slice(0, at) + "b" + doc.slice(at))
    const end = doc.indexOf('"x"') + 3
    expect(after({ changes: { from: end, insert: " " } })).toBe(doc.slice(0, end) + " " + doc.slice(end))
  })

  it("lets the whole document be replaced, and the editor's own replacement through", () => {
    expect(after({ changes: { from: 0, to: doc.length, insert: "{}" } })).toBe("{}")
    expect(after({ changes: { from: doc.indexOf('"x"'), to: doc.indexOf('"x"') + 3, insert: '"y"' }, filter: false })).toContain(
      '"format": "y"',
    )
  })
})

describe("completeJson", () => {
  const hints: JsonHints = {
    "": [{ key: "name", detail: "text" }, { key: "params" }],
    "/params": [{ key: "mode", values: ["fast", "safe", null] }, { key: "retries" }],
  }
  const complete = (doc: string, pos: number) => completeJson(new CompletionContext(stateOf(doc, [], hints), pos, true))

  it("offers the keys an object lacks where one can start", () => {
    const doc = '{"name": "a", }'
    const r = complete(doc, doc.length - 1)!
    expect(r.options.map((o) => o.label)).toEqual(['"params"'])
    expect(r.options[0].apply).toBe('"params": ')
    expect([r.from, r.to]).toEqual([doc.length - 1, doc.length - 1])
  })

  it("offers a nested object's keys, by its place", () => {
    const doc = '{"params": {}}'
    expect(complete(doc, doc.indexOf("{}") + 1)!.options.map((o) => o.label)).toEqual(['"mode"', '"retries"'])
  })

  it("completes a key being typed with its colon, and renames one that has it", () => {
    const typing = '{"params": {"mo"}}'
    const r = complete(typing, typing.indexOf("mo") + 2)!
    expect(r.options.find((o) => o.label === '"mode"')!.apply).toBe('"mode": ')
    expect(typing.slice(r.from, r.to)).toBe('"mo"')

    const renaming = '{"params": {"mo": 1}}'
    const s = complete(renaming, renaming.indexOf("mo") + 2)!
    expect(s.options.find((o) => o.label === '"mode"')!.apply).toBe('"mode"')
  })

  it("offers a key's values in place of the value there", () => {
    const doc = '{"params": {"mode": "f"}}'
    const r = complete(doc, doc.indexOf('"f"') + 2)!
    expect(r.options.map((o) => o.label)).toEqual(['"fast"', '"safe"', "null"])
    expect(doc.slice(r.from, r.to)).toBe('"f"')
  })

  it("offers nothing where nothing is hinted", () => {
    expect(complete('{"other": {}}', '{"other": {'.length)).toBeNull()
    const noValues = '{"params": {"retries": 1}}'
    expect(complete(noValues, noValues.indexOf("1") + 1)).toBeNull()
  })
})

describe("diagnosticsOf", () => {
  const doc = '{"name": "Bad", "params": {"mode": "slow", "list": [1]}}'
  const marks = (problems: { path: string; message: string }[], text = doc) =>
    diagnosticsOf(stateOf(text), problems).map((d) => [text.slice(d.from, d.to), d.message])

  it("marks a plain value where it is wrong, an object or array at its key", () => {
    expect(marks([{ path: "/params/mode", message: "No" }])).toEqual([['"slow"', "No"]])
    expect(marks([{ path: "/params/list", message: "No" }])).toEqual([['"list"', "No"]])
  })

  it("marks a missing key on the nearest part that is there, naming it", () => {
    expect(marks([{ path: "/params/base_url", message: "Required" }])).toEqual([['"params"', "base_url: Required"]])
    expect(marks([{ path: "/description", message: "Required" }])).toEqual([["{", "description: Required"]])
  })

  it("marks only where the text stops being JSON when it is not", () => {
    const broken = '{"name": "a",, "params": {}}'
    const d = diagnosticsOf(stateOf(broken), [{ path: "/name", message: "ignored" }])
    expect(d).toHaveLength(1)
    expect(d[0].from).toBe(broken.indexOf(",,") + 1)
    expect(d[0].message).toMatch(/^Not JSON: /)
  })
})

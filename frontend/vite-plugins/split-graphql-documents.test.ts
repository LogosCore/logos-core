import { readFileSync } from "node:fs"
import path from "node:path"
import { describe, expect, it } from "vitest"
import * as generated from "../src/graphql/gql/graphql"
import { CLASS_JS, CLASS_TS, splitDocuments } from "./split-graphql-documents"

const GENERATED = path.resolve(import.meta.dirname, "../src/graphql/gql/graphql.ts")
const source = readFileSync(GENERATED, "utf8")

type Document = { toString(): string; __meta__?: unknown }

/** The class's module, evaluated: what a document module imports. */
const Class = new Function(CLASS_JS.replace("export class", "return class"))() as new (
  value: string,
  meta?: unknown,
) => Document

/** A document's module, evaluated with that class. */
function evaluate(name: string, module: string): Document {
  const body = module.replace(/^import \{ TypedDocumentString \} from "[^"]+";\n/, "").replace(`export const ${name} = `, "return ")
  return new Function("TypedDocumentString", body)(Class) as Document
}

const originals = Object.entries(generated).filter(([, v]) => v instanceof generated.TypedDocumentString) as [
  string,
  Document,
][]

describe("each generated GraphQL document in a module of its own", () => {
  const split = splitDocuments(source)

  it("moves every document, the same text and the same meta", () => {
    expect(originals.length).toBeGreaterThan(200)
    expect(split.documents.size).toBe(originals.length)
    for (const [name, original] of originals) {
      const module = split.documents.get(name)
      expect(module, name).toBeDefined()
      const doc = evaluate(name, module!)
      expect(doc.toString(), name).toBe(original.toString())
      expect(String(doc), name).toBe(String(original))
      expect(JSON.stringify({ query: doc }), name).toBe(JSON.stringify({ query: original }))
      expect(doc.__meta__, name).toEqual(original.__meta__)
      expect(doc, name).toBeInstanceOf(String)
    }
  })

  it("leaves graphql.ts its types, re-exporting the class and each document from their modules", () => {
    expect(split.code).not.toContain("new TypedDocumentString(")
    expect(split.code).not.toContain("class TypedDocumentString")
    expect(split.code).toContain(`export { TypedDocumentString } from ${JSON.stringify("\0graphql-document-class")};`)
    for (const [name] of originals) {
      const reexport = `export { ${name} } from ${JSON.stringify(`\0graphql-document:${name}`)};`
      expect(split.code.split(reexport).length - 1, name).toBe(1)
    }
    // The types stay where they were.
    expect(split.code).toContain("export type Scalars = {")
  })

  it("marks each document free of side effects, for a chunk to leave out unused", () => {
    for (const [name, module] of split.documents) {
      expect(module, name).toContain(`export const ${name} = /*#__PURE__*/ new TypedDocumentString(`)
    }
  })

  it("refuses a file it does not read as the preset writes it", () => {
    expect(() => splitDocuments(source.replace("this.__meta__ = __meta__;", "this.meta = __meta__;"))).toThrow(/class/)
    const doc = originals[0][0]
    expect(() => splitDocuments(source.replace(`export const ${doc} = new TypedDocumentString(\``, `export const ${doc} = new TypedDocumentString(\`\${x}`))).toThrow(
      /interpolates/,
    )
    expect(() => splitDocuments(source + `\nconst stray = new TypedDocumentString("q");\n`)).toThrow(/documents of/)
  })

  it("behaves as the generated class does", () => {
    const doc = new Class("query Q { a }", { fragmentName: "F" })
    const original = new generated.TypedDocumentString("query Q { a }", { fragmentName: "F" })
    expect(doc.toString()).toBe(original.toString())
    expect(Object.keys(doc).sort()).toEqual(Object.keys(original).sort())
    expect(source).toContain(CLASS_TS)
  })
})

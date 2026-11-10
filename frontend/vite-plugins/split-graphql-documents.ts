import path from "node:path"
import type { Plugin } from "vite"

// The client preset writes every GraphQL document into one module,
// src/graphql/gql/graphql.ts: a TypedDocumentString constant each, beside
// the schema's types. A module goes to one chunk, and the app shell imports
// a few documents, so all of them rode on first paint: the chunk named
// graphql-client, 132 KB of the 1 048 KB critical path, most of it for
// pages not opened yet.
//
// Built, each document is a module of its own, free of side effects, which
// graphql.ts re-exports: a chunk carries the documents its code imports, and
// one nothing imports is left out. graphql.ts keeps its types and the code
// imports as before; the dev server and the tests read the file as written
// (the plugin is build-only).
//
// The generated file's shape is checked, not assumed: a class or a document
// that does not read as below fails the build, rather than ship a document
// changed or lost.

const CLASS_MODULE = "\0graphql-document-class"
const DOCUMENT_MODULE = "\0graphql-document:"

/** The class the client preset writes in string document mode, as it writes it. */
export const CLASS_TS = `export class TypedDocumentString<TResult, TVariables>
  extends String
  implements DocumentTypeDecoration<TResult, TVariables>
{
  __apiType?: NonNullable<DocumentTypeDecoration<TResult, TVariables>['__apiType']>;
  private value: string;
  public __meta__?: Record<string, any> | undefined;

  constructor(value: string, __meta__?: Record<string, any> | undefined) {
    super(value);
    this.value = value;
    this.__meta__ = __meta__;
  }

  override toString(): string & DocumentTypeDecoration<TResult, TVariables> {
    return this.value;
  }
}`

/** The same class in JavaScript, its fields defined as the app's TypeScript defines them (useDefineForClassFields). */
export const CLASS_JS = `export class TypedDocumentString extends String {
  __apiType;
  value;
  __meta__;
  constructor(value, __meta__) {
    super(value);
    this.value = value;
    this.__meta__ = __meta__;
  }
  toString() {
    return this.value;
  }
}
`

const DOCUMENT = /^export const (\w+) = new TypedDocumentString\(`/gm
// After a document's text: its meta (a fragment's name) or none, then the
// cast, to the end of its line (the file's last has no newline).
const DOCUMENT_TAIL = /^(?:, (\{[^\n]*?\}))?\) as unknown as TypedDocumentString<[^\n]*>;(?:\n|$)/

/** graphql.ts with its documents moved out, and each document's module, by its name. */
export interface SplitDocuments {
  code: string
  documents: Map<string, string>
}

/** Where a template literal that starts at start ends (its closing backtick); -1 when it interpolates or never ends. */
function templateEnd(code: string, start: number): number {
  for (let i = start; i < code.length; i++) {
    if (code[i] === "\\") i++
    else if (code[i] === "`") return i
    else if (code[i] === "$" && code[i + 1] === "{") return -1
  }
  return -1
}

/** Splits the generated module's source; throws on any part it does not read as the client preset writes it. */
export function splitDocuments(source: string): SplitDocuments {
  const fail = (what: string): never => {
    throw new Error(`split-graphql-documents: ${what} (vite-plugins/split-graphql-documents.ts reads graphql.ts as the client preset writes it)`)
  }
  const classAt = source.indexOf(CLASS_TS)
  if (classAt < 0) fail("TypedDocumentString is not the class this plugin knows")
  const code =
    source.slice(0, classAt) +
    `export { TypedDocumentString } from ${JSON.stringify(CLASS_MODULE)};` +
    source.slice(classAt + CLASS_TS.length)

  const documents = new Map<string, string>()
  let out = ""
  let at = 0
  for (const m of code.matchAll(DOCUMENT)) {
    const name = m[1]
    const textStart = m.index + m[0].length
    const textEnd = templateEnd(code, textStart)
    if (textEnd < 0) fail(`${name}'s text does not end, or interpolates`)
    const tail = DOCUMENT_TAIL.exec(code.slice(textEnd + 1))
    if (!tail) fail(`${name} does not end as a document does`)
    const meta = tail![1]
    if (meta !== undefined) JSON.parse(meta)
    if (documents.has(name)) fail(`${name} twice`)
    const text = code.slice(textStart, textEnd)
    documents.set(
      name,
      `import { TypedDocumentString } from ${JSON.stringify(CLASS_MODULE)};\n` +
        `export const ${name} = /*#__PURE__*/ new TypedDocumentString(\`${text}\`${meta === undefined ? "" : `, ${meta}`});\n`,
    )
    out += code.slice(at, m.index) + `export { ${name} } from ${JSON.stringify(DOCUMENT_MODULE + name)};\n`
    at = textEnd + 1 + tail![0].length
  }
  out += code.slice(at)
  const expected = source.match(/new TypedDocumentString\(/g)?.length ?? 0
  if (documents.size !== expected) fail(`read ${documents.size} documents of ${expected}`)
  return { code: out, documents }
}

/** The build plugin for the generated module at file (an absolute path). */
export function splitGraphqlDocuments(file: string): Plugin {
  let documents = new Map<string, string>()
  return {
    name: "split-graphql-documents",
    apply: "build",
    // Before the TypeScript transform: the source as the preset wrote it.
    enforce: "pre",
    resolveId(id) {
      if (id === CLASS_MODULE || id.startsWith(DOCUMENT_MODULE)) return { id, moduleSideEffects: false }
      return null
    },
    load(id) {
      if (id === CLASS_MODULE) return CLASS_JS
      if (!id.startsWith(DOCUMENT_MODULE)) return null
      const document = documents.get(id.slice(DOCUMENT_MODULE.length))
      if (document === undefined) this.error(`no GraphQL document ${id.slice(DOCUMENT_MODULE.length)}`)
      return document
    },
    transform(code, id) {
      if (path.resolve(id.split("?")[0]) !== file) return null
      const split = splitDocuments(code)
      documents = split.documents
      return { code: split.code, map: null }
    },
  }
}

// What a JSON editor (components/ui/json-editor.tsx) is told about the
// document it edits, by the record it edits: where the problems are, which
// parts are locked, what keys and values to offer. Places are JSON Pointers
// (RFC 6901): "" is the whole document, "/params/base_url" a key of the
// object under "params". Kept apart from the editor so the records' own code
// does not load it.

/** A problem with a part of the document. */
export interface JsonProblem {
  path: string
  message: string
}

/** A key to offer inside an object, and what to say about it. */
export interface JsonKeyHint {
  key: string
  /** Beside the key in the completion list, e.g. its type. */
  detail?: string
  /** What the key is for: in the completion list, and on hover. */
  info?: string
  /** Values to offer for it (an enum's options), as JSON values. */
  values?: readonly unknown[]
  /** Items to offer in its list (an enum list's options), each once. */
  items?: readonly JsonItemHint[]
}

/** An item a list's key offers. */
export interface JsonItemHint {
  /** The item, as a JSON value. */
  value: unknown
  /** Beside it in the completion list, e.g. its label. */
  detail?: string
  /** What it is, in the completion list. */
  info?: string
}

/**
 * Keys to offer, by the pointer of the object they belong in. A "*" stands
 * for any one key or index: "/connections/*" is every connection's object.
 */
export type JsonHints = Readonly<Record<string, readonly JsonKeyHint[]>>

/** The keys hints offers in the object at pointer: its own, else a "*" pattern's. */
export function hintsAt(hints: JsonHints, pointer: string): readonly JsonKeyHint[] | undefined {
  const exact = hints[pointer]
  if (exact) return exact
  const tokens = pointer.split("/")
  for (const [pattern, keys] of Object.entries(hints)) {
    if (!pattern.includes("*")) continue
    const want = pattern.split("/")
    if (want.length === tokens.length && want.every((t, i) => t === "*" || t === tokens[i])) return keys
  }
  return undefined
}

/** The pointer of a key or index under parent. */
export function childPointer(parent: string, token: string | number): string {
  return `${parent}/${String(token).replaceAll("~", "~0").replaceAll("/", "~1")}`
}

/** A JSON document's value, or why it is not JSON. */
export type JsonParse = { ok: true; value: unknown } | { ok: false; message: string }

export function parseJson(text: string): JsonParse {
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch (err) {
    return { ok: false, message: err instanceof Error ? err.message : String(err) }
  }
}

/** Indented two spaces, as the editors show a document. */
export function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

/**
 * A JSON document's text indented as formatJson indents a value, without
 * reading it into one: strings (escapes and all), numbers and key order stay
 * as they are, only the whitespace between them changes. For a record kept
 * as it was sent, which formatJson(JSON.parse(text)) would not keep: it writes
 * "&" as "&", 2.50 as 2.5, and rounds integers past 2^53. Text that is
 * not JSON comes back as it is.
 */
export function indentJsonText(text: string): string {
  if (!parseJson(text).ok) return text
  const pad = (depth: number) => "\n" + "  ".repeat(depth)
  const blank = (c: string) => c === " " || c === "\n" || c === "\r" || c === "\t"
  let out = ""
  let depth = 0
  let i = 0
  while (i < text.length) {
    const c = text[i]
    if (c === '"') {
      // To its closing quote, past every escaped character.
      let j = i + 1
      while (text[j] !== '"') j += text[j] === "\\" ? 2 : 1
      out += text.slice(i, j + 1)
      i = j + 1
    } else if (c === "{" || c === "[") {
      let j = i + 1
      while (blank(text[j])) j++
      if (text[j] === (c === "{" ? "}" : "]")) {
        out += c + text[j] // empty, as formatJson writes it
        i = j + 1
      } else {
        depth++
        out += c + pad(depth)
        i++
      }
    } else if (c === "}" || c === "]") {
      depth--
      out += pad(depth) + c
      i++
    } else if (c === ",") {
      out += "," + pad(depth)
      i++
    } else if (c === ":") {
      out += ": "
      i++
    } else {
      if (!blank(c)) out += c // a number's, true's, false's or null's
      i++
    }
  }
  return out
}

/** A text property's value: "" when absent or null, a problem when not text. */
export function textOf(value: unknown, path: string, at: (path: string, message: string) => void): string {
  if (typeof value === "string") return value
  if (value !== undefined && value !== null) at(path, "Must be text")
  return ""
}

export function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

/** Whether two JSON values are equal (key order aside). */
export function sameJSON(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((v, i) => sameJSON(v, b[i]))
  if (isRecord(a) && isRecord(b)) {
    const ka = Object.keys(a)
    return ka.length === Object.keys(b).length && ka.every((k) => sameJSON(a[k], b[k]))
  }
  return false
}

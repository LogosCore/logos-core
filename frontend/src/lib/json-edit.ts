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

/** The pointer of a path of keys and indexes from the document. */
export function pointerOf(path: readonly (string | number)[]): string {
  return path.reduce<string>((p, token) => childPointer(p, token), "")
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

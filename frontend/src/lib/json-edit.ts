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

/** Keys to offer, by the pointer of the object they belong in. */
export type JsonHints = Readonly<Record<string, readonly JsonKeyHint[]>>

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

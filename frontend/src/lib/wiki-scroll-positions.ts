// Where the operator last was in each of their most recently read wiki pages,
// so going back and forth between two long pages lands each one where it was
// left instead of at the top.
//
// Browser-local on purpose: a reading position is about this screen and this
// window width, not something to follow the operator to another machine, and
// losing it costs one scroll. Kept to the last MAX_REMEMBERED documents, most
// recent first.

const STORAGE_KEY = "wiki_scroll_positions"
export const MAX_REMEMBERED = 10

/**
 * A reading position. `block` and `offset` are the primary record: the index
 * of the top-level editor block at the top of the viewport and how far into
 * it, in pixels. They survive a change of window width, and content above
 * growing as it loads, where a raw pixel offset would not. `scrollTop` is the
 * fallback for when that block no longer exists (the page was shortened, or
 * the viewport was in the footer below the last block).
 */
export interface WikiScrollPosition {
  documentId: string
  block: number
  offset: number
  scrollTop: number
}

function isPosition(v: unknown): v is WikiScrollPosition {
  if (!v || typeof v !== "object") return false
  const p = v as Record<string, unknown>
  return (
    typeof p.documentId === "string" &&
    typeof p.block === "number" &&
    typeof p.offset === "number" &&
    typeof p.scrollTop === "number"
  )
}

function load(): WikiScrollPosition[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : []
    return Array.isArray(parsed) ? parsed.filter(isPosition) : []
  } catch {
    return []
  }
}

export function loadScrollPosition(documentId: string): WikiScrollPosition | null {
  return load().find((p) => p.documentId === documentId) ?? null
}

/** Record a position, moving that document to the front and capping the list. */
export function saveScrollPosition(position: WikiScrollPosition) {
  const next = [
    position,
    ...load().filter((p) => p.documentId !== position.documentId),
  ].slice(0, MAX_REMEMBERED)
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // Quota or private mode: the page just opens at the top, as it used to.
  }
}

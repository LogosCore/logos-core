// Where the operator was last looking on each of their recent drawings.
//
// Without this every drawing opened fitted to its content, so going back to a
// detail of a large diagram meant panning and zooming to it again on every
// visit. The prose counterpart is lib/wiki-scroll-positions.ts, and this
// follows it for the same reasons: browser-local, since a view belongs to
// this screen rather than to the drawing, and capped to the most recent few.
//
// Not shared through the room on purpose: scroll and zoom are per viewer
// (see SharedAppState in drawing-scene.ts).

const STORAGE_KEY = "wiki_drawing_views"
export const MAX_REMEMBERED_VIEWS = 20

export interface DrawingView {
  documentId: string
  scrollX: number
  scrollY: number
  zoom: number
}

function isView(v: unknown): v is DrawingView {
  if (!v || typeof v !== "object") return false
  const p = v as Record<string, unknown>
  return (
    typeof p.documentId === "string" &&
    Number.isFinite(p.scrollX) &&
    Number.isFinite(p.scrollY) &&
    // Excalidraw clamps zoom to 0.1..30; anything outside is not ours.
    typeof p.zoom === "number" &&
    p.zoom >= 0.1 &&
    p.zoom <= 30
  )
}

function load(): DrawingView[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : []
    return Array.isArray(parsed) ? parsed.filter(isView) : []
  } catch {
    return []
  }
}

export function loadDrawingView(documentId: string): DrawingView | null {
  return load().find((v) => v.documentId === documentId) ?? null
}

/** Record a view, moving that drawing to the front and capping the list. */
export function saveDrawingView(view: DrawingView) {
  if (!isView(view)) return
  const next = [view, ...load().filter((v) => v.documentId !== view.documentId)].slice(
    0,
    MAX_REMEMBERED_VIEWS,
  )
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // Quota or private mode: the drawing just opens fitted, as it used to.
  }
}

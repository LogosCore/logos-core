// Which file attachment previews are open and how tall, so reopening a wiki
// page restores the preview panels the operator left expanded.
//
// Same design as wiki-scroll-positions: browser-local, capped, keyed by fileId.
// A file that was removed or whose id changed simply has no state to restore.

const STORAGE_KEY = "wiki_file_preview_state"
const MAX_REMEMBERED = 50

export interface FilePreviewState {
  fileId: string
  height: number
}

function isEntry(v: unknown): v is FilePreviewState {
  if (!v || typeof v !== "object") return false
  const e = v as Record<string, unknown>
  return typeof e.fileId === "string" && typeof e.height === "number"
}

function load(): FilePreviewState[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    const parsed: unknown = raw ? JSON.parse(raw) : []
    return Array.isArray(parsed) ? parsed.filter(isEntry) : []
  } catch {
    return []
  }
}

export function loadFilePreviewState(fileId: string): FilePreviewState | null {
  return load().find((e) => e.fileId === fileId) ?? null
}

export function saveFilePreviewState(entry: FilePreviewState) {
  const next = [
    entry,
    ...load().filter((e) => e.fileId !== entry.fileId),
  ].slice(0, MAX_REMEMBERED)
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // Quota or private mode — preview just opens at default height next time.
  }
}

export function removeFilePreviewState(fileId: string) {
  const next = load().filter((e) => e.fileId !== fileId)
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // Best-effort.
  }
}

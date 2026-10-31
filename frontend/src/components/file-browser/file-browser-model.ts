import { samePath } from "@/graphql/hooks/file-browser"
import { crumbs } from "@/components/minion-sessions/dir-listing"
import { formatAgo } from "@/components/minion-sessions/sessions-layout"
import { formatDateTime } from "@/lib/date-format"

// What the file browser works out of what it reads (file-browser.tsx):
// tasks on their way, a folder's ancestors, how fresh a folder is.

export interface TaskLike {
  id: string
  command: string
  state: string
  args?: unknown
}

/** A task that is running on a path: what it does, as a row says it. */
export interface InFlight {
  taskId: string
  path: string
  action: string
  label: string
}

const LABELS: Record<string, string> = {
  list_dir: "listing…",
  delete_path: "deleting…",
  download_file: "downloading…",
  stream_file: "streaming…",
  upload_file: "uploading…",
  upload_stream: "uploading…",
}

const FINISHED = new Set(["DONE", "FAILED", "CANCELLED"])

/** The tasks not yet ended that act on a path, by their commands' actions. */
export function inFlight(tasks: readonly TaskLike[], actionOf: (command: string) => string | null | undefined): InFlight[] {
  const out: InFlight[] = []
  for (const t of tasks) {
    if (FINISHED.has(t.state)) continue
    const action = actionOf(t.command)
    const path = (t.args as Record<string, unknown> | null | undefined)?.path
    if (!action || !(action in LABELS) || typeof path !== "string" || path === "") continue
    out.push({ taskId: t.id, path, action, label: LABELS[action] })
  }
  return out
}

/** The one on a path, if any. */
export function inFlightAt(all: readonly InFlight[], path: string): InFlight | undefined {
  return all.find((f) => samePath(f.path, path))
}

/** The separator a host path is written with. */
export function sepOf(path: string): "/" | "\\" {
  return path.startsWith("\\\\") || /^[A-Za-z]:/.test(path) ? "\\" : "/"
}

/** The folders on the way to a path, from its root, with it. */
export function pathsTo(path: string): string[] {
  return crumbs(path, sepOf(path)).map((c) => c.path)
}

/** A path's folder; null for a root. */
export function parentOf(path: string): string | null {
  const all = pathsTo(path)
  return all.length > 1 ? all[all.length - 2] : null
}

export interface TaskRefLike {
  number: number
  user?: string | null
}

/** "#46 (root)": a task's session and its host user. */
export function byWhom(r: TaskRefLike | null | undefined): string {
  if (!r) return ""
  return r.user ? `#${r.number} (${r.user})` : `#${r.number}`
}

/** "3h ago", or the date and time from a day on. */
export function when(at: string, now = Date.now()): string {
  return formatAgo(now - Date.parse(at)) ?? formatDateTime(at)
}

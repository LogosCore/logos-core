import { samePath } from "@/graphql/hooks/file-browser"
import { crumbs } from "@/components/minion-sessions/dir-listing"
import { formatAgo } from "@/components/minion-sessions/sessions-layout"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { KIND_LABELS, volumeKind } from "@/components/minion-sessions/volumes"

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
  walk_dir: "walking…",
  list_volumes: "listing volumes…",
}

/** A list_volumes task on its way: the machine's roots, no path of its own. */
export const VOLUMES_PATH = ""

const FINISHED = new Set(["DONE", "FAILED", "CANCELLED"])

/** The tasks not yet ended that act on a path, by their commands' actions. */
export function inFlight(tasks: readonly TaskLike[], actionOf: (command: string) => string | null | undefined): InFlight[] {
  const out: InFlight[] = []
  for (const t of tasks) {
    if (FINISHED.has(t.state)) continue
    const action = actionOf(t.command)
    if (action === "list_volumes") {
      out.push({ taskId: t.id, path: VOLUMES_PATH, action, label: LABELS[action] })
      continue
    }
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

/** How a folder's icon looks: amber once listed; grey and dashed while only what other tasks put there is known. */
export const LISTED_FOLDER = "text-amber-600 dark:text-amber-400"
export const UNLISTED_FOLDER = "text-muted-foreground [stroke-dasharray:3_2]"
/** A folder a task could not read: its lock. */
export const UNREADABLE_FOLDER = "text-red-600 dark:text-red-400"
/** A file core has a copy of (a download, or a stream kept as it passed): its icon. */
export const DOWNLOADED_FILE = "text-sky-600 dark:text-sky-400"

/**
 * Whether an entry is one a task has taken in: a folder listed, a file core
 * has a copy of (downloaded, or streamed and kept); a root always. Its name
 * shows bright, and the rest dimmed: what is known only by name, or as the
 * way to something else. A file streamed with no copy kept stays dim: core
 * does not have it.
 */
export function takenIn(e: { type: string; parent?: string | null; listedAt?: string | null; downloaded: readonly unknown[] }): boolean {
  if (!e.parent) return true
  if (e.type === "dir") return !!e.listedAt
  if (e.type === "file") return e.downloaded.length > 0
  return false
}

/** What a folder's hover says of its listings: listed, never, or not read and why. */
export function listedSays(e: {
  listedAt?: string | null
  listedBy?: TaskRefLike | null
  listErr?: string | null
  listErrAt?: string | null
  listErrBy?: TaskRefLike | null
}): string {
  const listed = e.listedAt ? `Listed ${formatDateTime(e.listedAt)} by ${byWhom(e.listedBy)}` : "Never listed"
  if (!e.listErr) return listed
  const failed = `Could not be read${e.listErrAt ? ` ${formatDateTime(e.listErrAt)}` : ""}${e.listErrBy ? ` by ${byWhom(e.listErrBy)}` : ""}: ${e.listErr}`
  return e.listedAt ? `${failed}\n${listed}` : failed
}

export interface VolumeLike {
  kind: string
  device?: string | null
  label?: string | null
  fs?: string | null
  size?: number | null
  free?: number | null
  readOnly: boolean
  noMedia: boolean
  seenAt: string
  seenBy: TaskRefLike
}

/** The newest volumes listing the entries say: when, and by whom; null when none holds a volume. */
export function volumesListed(entries: readonly { volume?: VolumeLike | null }[]): VolumeLike | null {
  let newest: VolumeLike | null = null
  for (const e of entries) {
    if (e.volume && (!newest || Date.parse(e.volume.seenAt) > Date.parse(newest.seenAt))) newest = e.volume
  }
  return newest
}

/** A volume's space as a row says it: "12 GB free of 64 GB", its size alone, "No media"; "" when the host did not say. */
export function spaceSays(v: Pick<VolumeLike, "size" | "free" | "noMedia">): string {
  if (v.noMedia) return "No media"
  if (v.size == null) return ""
  return `${v.free != null ? `${formatBytes(v.free)} free of ` : ""}${formatBytes(v.size)}`
}

/** What a volume's hover says: its kind and label, file system, device, space, and the task that listed it. */
export function volumeSays(v: VolumeLike): string {
  const what = [`${KIND_LABELS[volumeKind(v.kind)]} volume`, v.label ? `"${v.label}"` : null, v.fs, v.readOnly ? "read-only" : null]
  return [
    what.filter(Boolean).join(", "),
    v.device ? `From ${v.device}` : null,
    spaceSays(v),
    `Volumes listed ${formatDateTime(v.seenAt)} by ${byWhom(v.seenBy)}`,
  ]
    .filter(Boolean)
    .join("\n")
}

/** "3h ago", or the date and time from a day on. */
export function when(at: string, now = Date.now()): string {
  return formatAgo(now - Date.parse(at)) ?? formatDateTime(at)
}

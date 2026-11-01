// A dir_walk task's data (docs: contracts/task-results-and-actions.md):
// where a walk through a directory stands, the latest part's.

export interface DirWalk {
  root: string
  folders: number
  entries: number
  /** Folders it could not read. */
  unread: number
  /** Folders it did not enter: deeper than it goes, past its limit, another file system, hidden. */
  skipped: number
  /** The folder it is reading; none once it ended. */
  current?: string
}

const count = (v: unknown) => (typeof v === "number" && v >= 0 ? v : 0)

/** The data as a walk, or null when it is not one (shown raw then). */
export function readDirWalk(data: unknown): DirWalk | null {
  if (typeof data !== "object" || data === null) return null
  const d = data as Record<string, unknown>
  if (typeof d.root !== "string" || d.root === "") return null
  return {
    root: d.root,
    folders: count(d.folders),
    entries: count(d.entries),
    unread: count(d.unread),
    skipped: count(d.skipped),
    current: typeof d.current === "string" && d.current !== "" ? d.current : undefined,
  }
}

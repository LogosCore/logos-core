import type { MinionTaskProcessesQuery } from "@/graphql/gql/graphql"

// A process_list task's processes, as its view shows them (docs:
// contracts/task-results-and-actions.md, process_list): read from core a
// page at a time, ordered and searched there; what the view makes of a
// process here: its folder, its CPU time, its notes.

export type ProcessRow = MinionTaskProcessesQuery["minionTaskProcesses"]["edges"][number]["node"]

/**
 * A process's folder, for List its folder: its path up to its last
 * separator, a drive's or a share's root kept whole (C:\ and \\nas\tools\),
 * / for a file at the root; null without a path, or one with no separator.
 */
export function folderOf(path: string | null | undefined, sep: string): string | null {
  if (!path) return null
  const at = path.lastIndexOf(sep)
  if (at < 0) return null
  const folder = path.slice(0, at)
  if (sep === "/") return folder === "" ? "/" : folder
  // A drive (C:), or a share (\\nas\tools), takes its separator back.
  if (/^[A-Za-z]:$/.test(folder) || /^\\\\[^\\]+\\[^\\]+$/.test(folder)) return folder + sep
  return folder
}

/** CPU time, as ps shows it: m:ss, h:mm:ss from an hour; "" for none. */
export function cpuTimeText(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return ""
  const total = Math.floor(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = String(total % 60).padStart(2, "0")
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`
}

/** A start time as kill_process takes it: unix seconds; 0 for none. */
export function startedSeconds(started: string | null | undefined): number {
  if (!started) return 0
  const ms = Date.parse(started)
  return Number.isNaN(ms) ? 0 : Math.floor(ms / 1000)
}

/**
 * The notes beside a process's name: its state where it is not running or
 * sleeping, and its architecture where it is not the host's (a 32-bit
 * process on 64-bit Windows).
 */
export function processNotes(p: Pick<ProcessRow, "state" | "arch">, hostArch: string | null | undefined): string[] {
  const notes: string[] = []
  if (p.state && p.state !== "running" && p.state !== "sleeping") notes.push(p.state)
  if (p.arch && hostArch && p.arch !== hostArch) notes.push(p.arch)
  return notes
}

/** Who a process runs as, with its integrity and its Windows session where the list has them. */
export function userText(p: Pick<ProcessRow, "user" | "integrity" | "session">): string {
  return [p.user, p.integrity, p.session !== null && p.session !== undefined ? `session ${p.session}` : null].filter(Boolean).join(" · ")
}

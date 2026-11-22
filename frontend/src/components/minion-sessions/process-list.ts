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

/** A note beside a process's name: its words, and what they mean (its title). */
export interface ProcessNote {
  text: string
  title: string
}

/** The states a note says, what each means (docs: process_list). */
const STATES: Record<string, string> = {
  waiting: "Waiting in the kernel, and no signal wakes it (D): on a disk or a network share, say",
  stopped: "Stopped: by a signal, or every thread of it suspended",
  zombie: "Ended, its parent yet to reap it: only its entry is left",
}

/** Architectures in words (the list says them as Go does: 386, arm, amd64, arm64). */
const ARCHES: Record<string, string> = { "386": "32-bit", arm: "32-bit ARM", amd64: "x64", arm64: "ARM64" }

/**
 * The notes beside a process's name: its state where it is not running or
 * sleeping, and its architecture where it is not the host's (a 32-bit
 * process on 64-bit Windows: "32-bit"), each with its meaning; windows,
 * the host is Windows (a list whose separator is "\\").
 */
export function processNotes(p: Pick<ProcessRow, "state" | "arch">, hostArch: string | null | undefined, windows = false): ProcessNote[] {
  const notes: ProcessNote[] = []
  if (p.state && p.state !== "running" && p.state !== "sleeping") {
    notes.push({ text: p.state, title: STATES[p.state] ?? `Its state: ${p.state}` })
  }
  if (p.arch && hostArch && p.arch !== hostArch) {
    const text = ARCHES[p.arch] ?? p.arch
    const host = ARCHES[hostArch] ?? hostArch
    // "An x64", "an ARM64"; "a 32-bit".
    const a = /^([aeio]|x\d)/i.test(text) ? "An" : "A"
    const wow = windows && (p.arch === "386" || p.arch === "arm") ? ": Windows runs it under WOW64, its layer for 32-bit programs" : ""
    notes.push({ text, title: `${a} ${text} program (${p.arch}) on this ${host} host${wow}` })
  }
  return notes
}

/**
 * Who a process runs as, with its Windows session, and its integrity where
 * it says more than who: Windows' levels; on Unix the level is its uid's
 * (root, or user), so only root for an account of another name (uid 0 as
 * "toor").
 */
export function userText(p: Pick<ProcessRow, "user" | "integrity" | "session">): string {
  const integrity = p.integrity === "user" || (p.integrity === "root" && p.user === "root") ? null : p.integrity
  return [p.user, integrity, p.session !== null && p.session !== undefined ? `session ${p.session}` : null].filter(Boolean).join(" · ")
}

/**
 * A line of the tree in a process's name cell, at a level's column: a rail
 * (top to bottom: that level's process has more shown after it), a tee
 * (├: its own corner, its parent's line going on below it), an elbow
 * (└: its own corner, the last of its parent's), or a stem (in its own
 * column, from its fold toggle down to its processes below).
 */
export interface TreeLine {
  level: number
  kind: "rail" | "tee" | "elbow" | "stem"
}

/** The lines that draw the tree to a process, as core says them: its guides' rails, its own corner (a root has none), its stem. */
export function treeLines(p: Pick<ProcessRow, "depth" | "last" | "guides" | "stem">): TreeLine[] {
  const lines: TreeLine[] = []
  p.guides.forEach((on, level) => {
    if (on) lines.push({ level, kind: "rail" })
  })
  if (p.depth > 0) lines.push({ level: p.depth - 1, kind: p.last ? "elbow" : "tee" })
  if (p.stem) lines.push({ level: p.depth, kind: "stem" })
  return lines
}

/** set with pid turned the other way: added where it was not in it, taken out where it was. */
export function toggled(set: ReadonlySet<number>, pid: number): ReadonlySet<number> {
  const next = new Set(set)
  if (!next.delete(pid)) next.add(pid)
  return next
}

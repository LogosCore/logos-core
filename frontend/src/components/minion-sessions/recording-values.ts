import type { RecordingsQuery } from "@/graphql/gql/graphql"
import { formatBytes } from "@/lib/format-bytes"
import type { Tone } from "./tunnel-values"

// A desktop or terminal task's recordings, as the Recordings tab shows them
// (docs: contracts/minion-messages/tunnels.md, Recordings): each desktop
// opened in the web app, or each terminal session, recorded by core as it
// went. A desktop's plays in Guacamole's own player, what was typed read
// from its key events; a terminal's (an asciicast) in xterm.js, as the
// Terminal tab shows one (cast-values.ts).

export type Recording = RecordingsQuery["recordings"]["recordings"][number]

/** A recording's file formats. */
export const GUAC = "guac"
export const ASCIICAST = "asciicast"

/** Whether it is a terminal's: an asciicast. */
export function isTerminalRecording(r: Pick<Recording, "format">): boolean {
  return r.format === ASCIICAST
}

export const RECORDING = "RECORDING"
export const SAVED = "SAVED"
export const FAILED = "FAILED"

/** Whether a recording plays: it ended, and holds something. */
export function playable(r: Pick<Recording, "state" | "size">): boolean {
  return r.state === SAVED && r.size > 0
}

/** Whether its file downloads: anything kept of it, ended or not. */
export function downloadable(r: Pick<Recording, "state" | "size">): boolean {
  return r.state !== FAILED && r.size > 0
}

/** A recording's state as the list says it: what it is, its tone, and why. */
export function recordingState(r: Pick<Recording, "state" | "end" | "msg" | "size" | "lost">): {
  label: string
  tone: Tone
  detail: string | null
} {
  const lost = r.lost > 0 ? `${formatBytes(r.lost)} lost: core's store did not take them` : null
  const said = (...parts: (string | null | undefined)[]) => parts.filter((p) => p).join("; ") || null
  if (r.state === RECORDING) return { label: "recording", tone: "good", detail: lost }
  if (r.state === FAILED) return { label: "did not open", tone: "bad", detail: r.msg ?? null }
  if (r.size === 0) return { label: "nothing recorded", tone: "muted", detail: said(r.msg, lost) }
  switch (r.end) {
    case "closed":
      return { label: "closed", tone: "muted", detail: lost }
    case "desktop":
      return { label: "ended by the desktop", tone: "muted", detail: said(r.msg, lost) }
    case "process":
      return { label: "process ended", tone: "muted", detail: said(r.msg, lost) }
    case "task_ended":
      return { label: "task ended", tone: "muted", detail: lost }
    case "reset":
      return { label: "lost by the minion", tone: "pending", detail: lost }
    case "core_stopped":
      return { label: "core stopped", tone: "pending", detail: said("its last seconds are lost", lost) }
  }
  return { label: "saved", tone: "muted", detail: lost }
}

/** A time in a recording, m:ss, h:mm:ss from an hour. */
export function formatClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = String(total % 60).padStart(2, "0")
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`
}

/** The name a recording's file is saved as, core's; a terminal's transcript (ext "txt") beside it. */
export function recordingFileName(r: Pick<Recording, "front" | "format" | "taskId" | "number">, ext?: string): string {
  if (isTerminalRecording(r)) return `terminal-${r.taskId.slice(0, 8)}-${r.number}.${ext ?? "cast"}`
  return `desktop-${r.front}-${r.taskId.slice(0, 8)}-${r.number}.${ext ?? "guac"}`
}

/** Who a recording's session was: its operator, else its client (a raw terminal's address). */
export function openedBy(r: Pick<Recording, "username" | "client">): string {
  return r.username || r.client
}

/** A key event of a recording, as its player reads them. */
export interface KeyEventLike {
  definition: { keysym: number; name: string; value?: string }
  pressed: boolean
  /** Milliseconds from the recording's start. */
  timestamp: number
}

/** A line of what was typed: where it began, and its text. */
export interface TypedLine {
  at: number
  text: string
}

// The modifiers, by keysym: what a key pressed with one of them names.
const MODIFIERS: Record<number, string> = {
  0xffe1: "Shift",
  0xffe2: "Shift",
  0xffe3: "Ctrl",
  0xffe4: "Ctrl",
  0xffe7: "Meta",
  0xffe8: "Meta",
  0xffe9: "Alt",
  0xffea: "Alt",
  0xfe03: "AltGr",
  0xffeb: "Super",
  0xffec: "Super",
}
const ORDER = ["Ctrl", "Alt", "Super", "Meta", "Shift"]

/**
 * What was typed in a recording, line by line: each key pressed, its
 * character, or its name in brackets (<Backspace>, <Tab>, <Left>), or with
 * the modifiers held (<Ctrl+C>: Shift alone types its own characters,
 * AltGr too). A line ends at Enter, or where typing paused for gap ms.
 */
export function typedLines(events: readonly KeyEventLike[], gap = 2000): TypedLine[] {
  const lines: TypedLine[] = []
  const held = new Map<number, string>()
  let line: TypedLine | null = null
  let last = -Infinity
  const end = () => {
    if (line && line.text !== "") lines.push(line)
    line = null
  }
  for (const e of events) {
    const keysym = e.definition.keysym
    const modifier = MODIFIERS[keysym]
    if (modifier) {
      if (e.pressed) held.set(keysym, modifier)
      else held.delete(keysym)
      continue
    }
    if (!e.pressed) continue
    if (e.timestamp - last > gap) end()
    last = e.timestamp
    const mods = ORDER.filter((m) => [...held.values()].includes(m))
    const chord = mods.some((m) => m !== "Shift")
    const value = e.definition.value
    let text: string
    if (chord) {
      const name = e.definition.name.length === 1 ? e.definition.name.toUpperCase() : e.definition.name
      text = `<${[...mods, name].join("+")}>`
    } else if (value !== undefined) {
      text = value
    } else {
      text = `<${e.definition.name}>`
    }
    line ??= { at: e.timestamp, text: "" }
    if (text === "\n") {
      line.text += "⏎"
      end()
      continue
    }
    line.text += text
  }
  end()
  return lines
}

/** The line playing at position: the last begun by then; -1 for none. */
export function lineAt(lines: readonly TypedLine[], position: number): number {
  let lo = 0
  let hi = lines.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (lines[mid].at <= position) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found
}

/** A desktop setting as the settings form has it. */
export interface SettingField {
  key: string
  label: string
  default?: unknown
  options?: readonly { value: string; label?: string | null }[] | null
}

/**
 * The settings a recording's desktop opened with that are not their
 * defaults, as its front's form says them ("Quality: Best", "View only"):
 * in the form's order, keys it does not know last, as they were sent.
 */
export function settingsSummary(fields: readonly SettingField[], settings: unknown): string[] {
  if (!settings || typeof settings !== "object" || Array.isArray(settings)) return []
  const values = settings as Record<string, unknown>
  const out: string[] = []
  const known = new Set<string>()
  for (const f of fields) {
    known.add(f.key)
    if (!(f.key in values)) continue
    const v = values[f.key]
    if (v === null || v === "" || v === f.default) continue
    if (v === true) {
      out.push(f.label)
    } else if (v === false) {
      out.push(`${f.label}: off`)
    } else {
      const option = f.options?.find((o) => o.value === v)
      out.push(`${f.label}: ${option?.label || String(v)}`)
    }
  }
  for (const [k, v] of Object.entries(values)) {
    if (!known.has(k) && v !== null && v !== "") out.push(`${k}: ${String(v)}`)
  }
  return out
}

import type { TypedLine } from "./recording-values"

// A terminal's recording (docs: contracts/minion-messages/tunnels.md,
// Recordings), an asciicast v2 as core keeps it: a JSON header line (the
// terminal's size, when it began, a title), then an event a line,
// [seconds, code, data]: "o" what the process wrote, "i" what was typed,
// "r" a new size ("COLSxROWS"). Read here for the player, the keys typed
// and the transcript.

export interface CastHeader {
  version: number
  width: number
  height: number
  timestamp?: number
  title?: string
}

export interface CastEvent {
  /** Milliseconds from its start. */
  at: number
  code: string
  data: string
}

export interface Cast {
  header: CastHeader
  events: CastEvent[]
}

/**
 * An asciicast's header and events. A line that does not read as an event
 * is passed over (the last, cut, of one still recording); a header that
 * is not an asciicast v2's throws.
 */
export function parseCast(text: string): Cast {
  const lines = text.split("\n")
  let header: CastHeader
  try {
    header = JSON.parse(lines[0]) as CastHeader
  } catch {
    throw new Error("not an asciicast: its first line is not JSON")
  }
  if (!header || header.version !== 2 || !(header.width > 0) || !(header.height > 0)) {
    throw new Error("not an asciicast v2: its header says no version 2, or no size")
  }
  const events: CastEvent[] = []
  for (let i = 1; i < lines.length; i++) {
    const line = lines[i]
    if (line === "") continue
    let e: unknown
    try {
      e = JSON.parse(line)
    } catch {
      continue
    }
    if (!Array.isArray(e) || e.length < 3 || typeof e[0] !== "number" || typeof e[1] !== "string" || typeof e[2] !== "string") continue
    events.push({ at: Math.round(e[0] * 1000), code: e[1], data: e[2] })
  }
  return { header, events }
}

/** A size event's data ("120x40"): null when it is not one. */
export function parseSize(data: string): { cols: number; rows: number } | null {
  const m = /^(\d+)x(\d+)$/.exec(data)
  if (!m) return null
  const cols = Number(m[1])
  const rows = Number(m[2])
  return cols > 0 && rows > 0 ? { cols, rows } : null
}

/**
 * Where each event plays, in milliseconds: when it came, or, skipping
 * pauses, with every pause longer than maxPause played as maxPause.
 */
export function playTimes(events: readonly CastEvent[], maxPause: number | null): number[] {
  const out: number[] = []
  let shift = 0
  let prev = 0
  for (const e of events) {
    const at = Math.max(e.at, prev)
    if (maxPause !== null && at - prev > maxPause) shift += at - prev - maxPause
    out.push(at - shift)
    prev = at
  }
  return out
}

/** The events played by position: how many of them (times ascending). */
export function playedBy(times: readonly number[], position: number): number {
  let lo = 0
  let hi = times.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (times[mid] <= position) lo = mid + 1
    else hi = mid
  }
  return lo
}

// What a key sends a terminal, by its name.
const SEQUENCES: Record<string, string> = {
  "\x1b[A": "Up",
  "\x1b[B": "Down",
  "\x1b[C": "Right",
  "\x1b[D": "Left",
  "\x1bOA": "Up",
  "\x1bOB": "Down",
  "\x1bOC": "Right",
  "\x1bOD": "Left",
  "\x1b[H": "Home",
  "\x1b[F": "End",
  "\x1bOH": "Home",
  "\x1bOF": "End",
  "\x1b[1~": "Home",
  "\x1b[4~": "End",
  "\x1b[2~": "Insert",
  "\x1b[3~": "Delete",
  "\x1b[5~": "PageUp",
  "\x1b[6~": "PageDown",
  "\x1b[Z": "Shift+Tab",
  "\x1b": "Esc",
}

// An escape sequence, a control character, or text up to the next of one:
// control characters are what it reads.
// eslint-disable-next-line no-control-regex
const TOKEN = /\x1b(?:\[[0-9;?]*[~A-Za-z]|O[A-Za-z])?|[\x00-\x1f\x7f]|[^\x00-\x1f\x7f\x1b]+/g

/** A control character's key: <Tab>, <Backspace>, <Ctrl+C>. */
function controlName(c: string): string {
  const code = c.charCodeAt(0)
  if (c === "\t") return "<Tab>"
  if (c === "\x7f" || c === "\b") return "<Backspace>"
  if (code === 0) return "<Ctrl+@>"
  if (code < 0x1b) return `<Ctrl+${String.fromCharCode(code + 0x40)}>`
  return `<Ctrl+${"[\\]^_"[code - 0x1b]}>`
}

/**
 * What was typed in a terminal's recording, line by line: each event's
 * text, its keys named in brackets (<Backspace>, <Up>, <Ctrl+C>). A line
 * ends at Enter (⏎), or where typing paused for gap ms; times are where
 * each line plays (playTimes).
 */
export function inputLines(events: readonly CastEvent[], times: readonly number[], gap = 2000): TypedLine[] {
  const lines: TypedLine[] = []
  let line: TypedLine | null = null
  let last = -Infinity
  const end = () => {
    if (line && line.text !== "") lines.push(line)
    line = null
  }
  events.forEach((e, i) => {
    if (e.code !== "i") return
    const at = times[i]
    if (at - last > gap) end()
    last = at
    for (const [token] of e.data.matchAll(TOKEN)) {
      line ??= { at, text: "" }
      if (token === "\r" || token === "\n") {
        line.text += "⏎"
        end()
        continue
      }
      if (token.startsWith("\x1b")) {
        line.text += `<${SEQUENCES[token] ?? `Esc${token.slice(1)}`}>`
      } else if (token.length === 1 && (token.charCodeAt(0) < 0x20 || token === "\x7f")) {
        line.text += controlName(token)
      } else {
        line.text += token
      }
    }
  })
  end()
  return lines
}

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
// control characters are what it reads. A CSI sequence's last character
// may be _, win32-input-mode's.
// eslint-disable-next-line no-control-regex
const TOKEN = /\x1b(?:\[[0-9;?]*[~A-Za-z_]|O[A-Za-z])?|[\x00-\x1f\x7f]|[^\x00-\x1f\x7f\x1b]+/g

/** A control character's key: <Tab>, <Backspace>, <Ctrl+C>. */
function controlName(c: string): string {
  const code = c.charCodeAt(0)
  if (c === "\t") return "<Tab>"
  if (c === "\x7f" || c === "\b") return "<Backspace>"
  if (code === 0) return "<Ctrl+@>"
  if (code < 0x1b) return `<Ctrl+${String.fromCharCode(code + 0x40)}>`
  return `<Ctrl+${"[\\]^_"[code - 0x1b]}>`
}

// A key of win32-input-mode: what a terminal sends a Windows console that
// asked for it (ConPTY's ESC[?9001h, as a BOB minion's on Windows does), a
// key's every press and release, ESC [ Vk ; Sc ; Uc ; Kd ; Cs ; Rc _: its
// virtual key, scan code, character (a UTF-16 unit, 0 for none), down
// (1) or up, its modifiers, its repeat count. A field left out is 0 (Rc:
// 1). An escape sequence is what it reads.
// eslint-disable-next-line no-control-regex
const WIN32_KEY =/^\x1b\[([0-9;]*)_$/

// A key that types no character, by its virtual key.
const VIRTUAL_KEYS: Record<number, string> = {
  0x21: "PageUp",
  0x22: "PageDown",
  0x23: "End",
  0x24: "Home",
  0x25: "Left",
  0x26: "Up",
  0x27: "Right",
  0x28: "Down",
  0x2d: "Insert",
  0x2e: "Delete",
}

// Its modifiers (Cs): either Alt, either Ctrl, Shift.
const ALT = 0x01 | 0x02
const CTRL = 0x04 | 0x08

/**
 * What a win32-input-mode key typed, as inputLines says keys: its character,
 * its name in brackets (<Up>, <F5>, <Ctrl+C>, <Alt+x>), "\r" for Enter, ""
 * for a release or a modifier alone. Shift types its characters; Ctrl with
 * Alt (AltGr) types its own.
 */
function win32Key(params: string): string {
  const [vk = 0, , uc = 0, kd = 0, cs = 0, rc = 1] = params.split(";").map((f) => (f === "" ? undefined : Number(f)))
  if (kd !== 1) return ""
  const ctrl = (cs & CTRL) !== 0 && (cs & ALT) === 0
  const alt = (cs & ALT) !== 0 && (cs & CTRL) === 0
  let key: string
  if (uc === 13) return "\r"
  if (uc === 27) {
    key = "<Esc>"
  } else if (uc > 0 && (uc < 0x20 || uc === 0x7f)) {
    key = controlName(String.fromCharCode(uc))
  } else if (uc > 0) {
    const c = String.fromCharCode(uc)
    key = ctrl ? `<Ctrl+${c.toUpperCase()}>` : alt ? `<Alt+${c}>` : c
  } else if (VIRTUAL_KEYS[vk]) {
    key = `<${VIRTUAL_KEYS[vk]}>`
  } else if (vk >= 0x70 && vk <= 0x87) {
    key = `<F${vk - 0x6f}>`
  } else {
    // Shift, Ctrl, Alt, the Windows keys, Caps Lock alone: nothing typed.
    return ""
  }
  return key.repeat(Math.min(Math.max(rc, 1), 100))
}

/** A token as inputLines says it: "\r" for Enter, "" for nothing typed. */
function tokenText(token: string): string {
  if (token === "\r" || token === "\n") return "\r"
  if (token.startsWith("\x1b")) {
    const win32 = WIN32_KEY.exec(token)
    if (win32) return win32Key(win32[1])
    return `<${SEQUENCES[token] ?? `Esc${token.slice(1)}`}>`
  }
  if (token.length === 1 && (token.charCodeAt(0) < 0x20 || token === "\x7f")) return controlName(token)
  return token
}

/**
 * What was typed in a terminal's recording, line by line: each event's
 * text, its keys named in brackets (<Backspace>, <Up>, <Ctrl+C>), a
 * win32-input-mode client's keys as the same (a Windows console asks for
 * that mode). A line ends at Enter (⏎), or where typing paused for gap ms;
 * times are where each line plays (playTimes), from its first key typed.
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
      const text = tokenText(token)
      if (text === "") continue
      line ??= { at, text: "" }
      if (text === "\r") {
        line.text += "⏎"
        end()
        continue
      }
      line.text += text
    }
  })
  end()
  return lines
}

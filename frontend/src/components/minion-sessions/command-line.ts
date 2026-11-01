// What an operator types in a session's console, read as a command and its
// args (the command's params, as the build schema declares them):
//
//   ls /var/log                 positional words fill the params in order
//   sleep 30s 20
//   sleep 30s --jitter=20       --key=value (or --key value) sets any param
//   shell ls -la /tmp | wc -l   a command whose first param is text takes
//   shell sleep 9 --timeout=1m    the rest of the line as it, verbatim; only
//                                 options at the end are read as options
//
// Words may be quoted ("a b" or 'a b'). Values are converted to the param's
// type here (a number, a list split at commas); core checks them again. A
// param of the connection format takes a session's connection by name or
// place (connection-choices.ts).

import {
  CONNECTION_FORMAT,
  connectionFromText,
  connectionText,
  matchConnections,
  type ConnectionChoice,
} from "./connection-choices"

export interface CommandParam {
  key: string
  type: string
  label: string
  required: boolean
  default?: unknown
  /** What the value is beyond its type: "connection" takes a session's connection. */
  format?: string | null
}

export interface CommandDef {
  id: string
  label: string
  description?: string | null
  params: readonly CommandParam[]
}

export type ParsedLine =
  | { ok: true; command: CommandDef; args: Record<string, unknown> }
  | { ok: false; error: string; command?: CommandDef }

const TEXT_TYPES = new Set(["string", "url", "secret", "enum"])

/** Whether a command takes the rest of the line as its first param. */
export function takesRest(command: CommandDef): boolean {
  return command.params.length > 0 && TEXT_TYPES.has(command.params[0].type)
}

interface Word {
  text: string
  start: number
  end: number
}

/** Words, quote-aware, with where each is in the line. */
export function words(line: string): Word[] {
  const out: Word[] = []
  let i = 0
  while (i < line.length) {
    while (i < line.length && /\s/.test(line[i])) i++
    if (i >= line.length) break
    const start = i
    let text = ""
    while (i < line.length && !/\s/.test(line[i])) {
      const c = line[i]
      if (c === '"' || c === "'") {
        const close = line.indexOf(c, i + 1)
        if (close < 0) {
          text += line.slice(i + 1)
          i = line.length
          break
        }
        text += line.slice(i + 1, close)
        i = close + 1
      } else {
        text += c
        i++
      }
    }
    out.push({ text, start, end: i })
  }
  return out
}

/** The command a line starts with: its first word, case aside. */
export function commandOf(line: string, commands: readonly CommandDef[]): CommandDef | undefined {
  const first = words(line)[0]?.text.toLowerCase()
  return first ? commands.find((c) => c.id.toLowerCase() === first) : undefined
}

/** A param's value from its text, in its type. */
function convert(
  param: CommandParam,
  text: string,
  connections: readonly ConnectionChoice[],
): { ok: true; value: unknown } | { ok: false; error: string } {
  if (param.format === CONNECTION_FORMAT) return connectionFromText(param.key, text, connections)
  switch (param.type) {
    case "int":
    case "bytes": {
      if (!/^[-+]?\d+$/.test(text)) return { ok: false, error: `${param.key}: a whole number` }
      return { ok: true, value: parseInt(text, 10) }
    }
    case "number": {
      const n = Number(text)
      return Number.isFinite(n) && text.trim() !== "" ? { ok: true, value: n } : { ok: false, error: `${param.key}: a number` }
    }
    case "bool": {
      const t = text.toLowerCase()
      if (["true", "yes", "on", "1"].includes(t)) return { ok: true, value: true }
      if (["false", "no", "off", "0"].includes(t)) return { ok: true, value: false }
      return { ok: false, error: `${param.key}: true or false` }
    }
    case "string_list":
    case "enum_list":
      return { ok: true, value: text.split(",").map((s) => s.trim()).filter((s) => s !== "") }
    case "string_map": {
      const out: Record<string, string> = {}
      for (const pair of text.split(",")) {
        if (pair.trim() === "") continue
        const eq = pair.indexOf("=")
        if (eq <= 0) return { ok: false, error: `${param.key}: key=value pairs, comma-separated` }
        out[pair.slice(0, eq).trim()] = pair.slice(eq + 1).trim()
      }
      return { ok: true, value: out }
    }
  }
  // Text, a duration ("30s"), a datetime (RFC 3339): as typed.
  return { ok: true, value: text }
}

/**
 * A line's command and each param's text as typed, unconverted and
 * unchecked: what a command form opens with (Shift+Enter). Words past the
 * params are extra.
 */
export interface LineTexts {
  command?: CommandDef
  texts: Map<string, string>
  extra: string[]
}

/** Reads a line's command and its params' texts, leniently (LineTexts). */
export function lineTexts(line: string, commands: readonly CommandDef[]): LineTexts {
  const all = words(line)
  const command = commandOf(line, commands)
  if (!command) return { texts: new Map(), extra: [] }
  const byKey = new Map(command.params.map((p) => [p.key, p]))
  const texts = new Map<string, string>()
  const extra: string[] = []
  const rest = all.slice(1)

  // An option: --key=value, or --key value. Only a param's key makes one.
  const option = (ws: Word[], i: number): { key: string; value: string; used: number } | null => {
    const m = /^--([A-Za-z0-9_.-]+)(?:=(.*))?$/.exec(ws[i].text)
    if (!m || !byKey.has(m[1])) return null
    if (m[2] !== undefined) return { key: m[1], value: m[2], used: 1 }
    if (i + 1 < ws.length) return { key: m[1], value: ws[i + 1].text, used: 2 }
    return null
  }

  if (takesRest(command)) {
    // Options at the end only: the text before is the first param, verbatim.
    let cut = rest.length
    for (;;) {
      let found = false
      for (const back of [1, 2]) {
        const i = cut - back
        if (i < 0) continue
        const o = option(rest, i)
        if (o && i + o.used === cut) {
          texts.set(o.key, o.value)
          cut = i
          found = true
          break
        }
      }
      if (!found) break
    }
    if (cut > 0) {
      let text = line.slice(rest[0].start, rest[cut - 1].end).trim()
      // A text wholly in quotes is unquoted.
      if (cut === 1) text = rest[0].text
      texts.set(command.params[0].key, text)
    }
  } else {
    const positional: string[] = []
    for (let i = 0; i < rest.length; ) {
      const o = option(rest, i)
      if (o) {
        texts.set(o.key, o.value)
        i += o.used
        continue
      }
      positional.push(rest[i].text)
      i++
    }
    const free = command.params.filter((p) => !texts.has(p.key))
    positional.forEach((text, i) => (i < free.length ? texts.set(free[i].key, text) : extra.push(text)))
  }
  return { command, texts, extra }
}

/** Reads a line as one of commands with its args; connections are the session's, for a connection param. */
export function parseCommandLine(
  line: string,
  commands: readonly CommandDef[],
  connections: readonly ConnectionChoice[] = [],
): ParsedLine {
  const all = words(line)
  if (all.length === 0) return { ok: false, error: "Type a command" }
  const { command, texts, extra } = lineTexts(line, commands)
  if (!command) {
    return { ok: false, error: `No command "${all[0].text}": ${commands.map((c) => c.id).join(", ") || "none"}` }
  }
  if (extra.length > 0) {
    return { ok: false, command, error: `${command.id} takes ${command.params.length === 0 ? "nothing" : `at most ${command.params.length}`}: ${usage(command)}` }
  }
  const byKey = new Map(command.params.map((p) => [p.key, p]))
  const args: Record<string, unknown> = {}
  for (const [key, text] of texts) {
    const r = convert(byKey.get(key)!, text, connections)
    if (!r.ok) return { ok: false, command, error: r.error }
    args[key] = r.value
  }
  const missing = command.params.filter((p) => p.required && (p.default === null || p.default === undefined) && !(p.key in args))
  if (missing.length > 0) {
    return { ok: false, command, error: `Missing ${missing.map((p) => p.key).join(", ")}: ${usage(command)}` }
  }
  return { ok: true, command, args }
}

/** How a command is typed: "sleep <interval> [jitter=10]". */
export function usage(command: CommandDef): string {
  const parts = command.params.map((p) => {
    if (p.required && (p.default === null || p.default === undefined)) return `<${p.key}>`
    const d = p.default === null || p.default === undefined ? "" : `=${String(p.default)}`
    return `[${p.key}${d}]`
  })
  return [command.id, ...parts].join(" ")
}

/**
 * A task as a line again: its command and args, as typed; a connection
 * param by its name, given the session's connections.
 */
export function lineOf(
  command: string,
  args: Record<string, unknown> | null | undefined,
  def?: CommandDef,
  connections: readonly ConnectionChoice[] = [],
): string {
  const parts = [command]
  const entries = Object.entries(args ?? {}).map(([k, v]): [string, unknown] => {
    const p = def?.params.find((x) => x.key === k)
    return [k, p?.format === CONNECTION_FORMAT && connections.length > 0 ? connectionText(v, connections) : v]
  })
  const quote = (s: string) => (/[\s"']/.test(s) || s === "" ? JSON.stringify(s) : s)
  const show = (v: unknown) => (Array.isArray(v) ? v.join(",") : typeof v === "object" && v !== null ? Object.entries(v).map(([k, x]) => `${k}=${String(x)}`).join(",") : String(v))
  if (def && takesRest(def)) {
    const first = def.params[0].key
    if (first in (args ?? {})) parts.push(String(args![first]))
    for (const [k, v] of entries) if (k !== first) parts.push(`--${k}=${quote(show(v))}`)
    return parts.join(" ")
  }
  for (const [k, v] of entries) parts.push(`--${k}=${quote(show(v))}`)
  return parts.join(" ")
}

/**
 * The lines a command line's ↑ and ↓ go through, oldest first: each line
 * once, where it was last run, the last max of them.
 */
export function commandHistory(lines: readonly string[], max = 200): string[] {
  const last = new Map<string, number>()
  lines.forEach((l, i) => last.set(l, i))
  return lines.filter((l, i) => last.get(l) === i).slice(-max)
}

/** Values the word being typed could be, and where it starts in the line. */
export interface ValueCompletion {
  key: string
  /** Where the word starts: a completion replaces the line from here. */
  start: number
  partial: string
  options: ConnectionChoice[]
}

/**
 * What the word at the end of the line could be, when it is a connection
 * param's value (positional, --key=value or --key value): the session's
 * connections it could name. Null for any other word.
 */
export function valueCompletion(
  line: string,
  commands: readonly CommandDef[],
  connections: readonly ConnectionChoice[],
): ValueCompletion | null {
  if (connections.length === 0) return null
  const command = commandOf(line, commands)
  if (!command || takesRest(command)) return null
  const all = words(line)
  const trailing = /\s$/.test(line)
  // Still typing the command.
  if (all.length < 2 && !trailing) return null
  const current: Word = trailing ? { text: "", start: line.length, end: line.length } : all[all.length - 1]
  const before = trailing ? all.slice(1) : all.slice(1, -1)
  const byKey = new Map(command.params.map((p) => [p.key, p]))
  const offer = (param: CommandParam | undefined, start: number, partial: string): ValueCompletion | null =>
    param?.format === CONNECTION_FORMAT ? { key: param.key, start, partial, options: matchConnections(partial, connections) } : null

  const eq = /^--([A-Za-z0-9_.-]+)=(.*)$/.exec(current.text)
  if (eq) return offer(byKey.get(eq[1]), current.start + eq[1].length + 3, eq[2])
  if (current.text.startsWith("--")) return null
  // --key value: the word before names the param.
  const prev = before.length > 0 ? /^--([A-Za-z0-9_.-]+)$/.exec(before[before.length - 1].text) : null
  if (prev && byKey.has(prev[1])) return offer(byKey.get(prev[1]), current.start, current.text)

  const set = new Set<string>()
  let positional = 0
  for (let i = 0; i < before.length; ) {
    const o = /^--([A-Za-z0-9_.-]+)(?:=(.*))?$/.exec(before[i].text)
    if (o && byKey.has(o[1])) {
      set.add(o[1])
      i += o[2] !== undefined ? 1 : 2
      continue
    }
    positional++
    i++
  }
  const free = command.params.filter((p) => !set.has(p.key))
  return offer(free[positional], current.start, current.text)
}

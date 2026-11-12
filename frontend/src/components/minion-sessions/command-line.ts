// What an operator types in a session's console, read as a command and its
// args (the command's params, as the build schema declares them):
//
//   ls /var/log                 words fill the params in the form's order
//   sleep 30s 20                  (its own, then More options), the order
//                                 usage and help show
//   sleep 30s --jitter=20       --key=value (or --key value) sets any param
//   feedback disk is full       an optional param takes a word that is one
//   feedback error disk full      of its values, when words are left for the
//   tunnel 1080                   required params after it; else it is passed
//                                 over (feedback's level, a tunnel's front)
//   shell ls -la /tmp | wc -l   the last required param, when it is text,
//   shell sleep 9 --timeout=1m    takes the rest of the line, verbatim;
//                                 options go before it or at the end
//
// A param that does not apply as the line stands (its when) takes no word.
// Words may be quoted ("a b" or 'a b'). Values are converted to the param's
// type here (a number, a list split at commas); core checks them again. A
// param of the connection format takes a session's connection by name or
// place (connection-choices.ts).

import { parseGoDuration } from "@/components/modules/management/field-values"
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
  /** When it applies: while the param key holds one of values (a tunnel's
   *  host, with front forward alone); always when absent. */
  when?: { key: string; values: readonly string[] } | null
  /** An enum's values. */
  options?: readonly string[] | null
}

/**
 * Whether a param applies, given the values (or the texts) typed so far:
 * always without a condition; with one, while the param it names holds one
 * of its values, as typed or else by its default. Core refuses one that
 * does not.
 */
export function paramApplies(param: CommandParam, params: readonly CommandParam[], values: ReadonlyMap<string, unknown> | Record<string, unknown>): boolean {
  const when = param.when
  if (!when) return true
  const typed = values instanceof Map ? values.get(when.key) : (values as Record<string, unknown>)[when.key]
  const value = typed ?? params.find((p) => p.key === when.key)?.default
  return typeof value === "string" && when.values.includes(value)
}

/** A condition as core says it: "front is forward", "front is socks, http or forward". */
export function conditionText(when: { key: string; values: readonly string[] }): string {
  const v = when.values
  return `${when.key} is ${v.length < 2 ? v.join("") : `${v.slice(0, -1).join(", ")} or ${v[v.length - 1]}`}`
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

/** The types of free text: a path, a command line, a message. */
const FREE_TEXT = new Set(["string", "url", "secret"])

const BOOL_WORDS = ["true", "yes", "on", "1", "false", "no", "off", "0"]

function isRequired(p: CommandParam): boolean {
  return p.required && (p.default === null || p.default === undefined)
}

/**
 * The param that takes the rest of the line, as typed: the last required
 * one, when it is free text (a path, a shell's command line, a message);
 * with none required, the first, when it is (ls's path). One that applies
 * only by a condition (when) never is. Params after it go as options.
 */
export function restParam(command: CommandDef): CommandParam | undefined {
  const required = command.params.filter((p) => isRequired(p) && !p.when)
  const p = required.length > 0 ? required[required.length - 1] : command.params[0]
  return p && !p.when && FREE_TEXT.has(p.type) ? p : undefined
}

/**
 * Whether a word reads as a value of an optional param: one that does not
 * passes it over (feedback's level, for a message; a tunnel's front, for
 * a port). One of an enum's options, a number, a duration, true or false,
 * a session's connection; free text takes any word.
 */
function fits(param: CommandParam, text: string, connections: readonly ConnectionChoice[]): boolean {
  if (param.format === CONNECTION_FORMAT) return connections.length === 0 || connectionFromText(param.key, text, connections).ok
  switch (param.type) {
    case "enum":
      return !param.options?.length || param.options.includes(text)
    case "bool":
      return BOOL_WORDS.includes(text.toLowerCase())
    case "int":
    case "bytes":
      return /^[-+]?\d+$/.test(text)
    case "number":
      return text.trim() !== "" && Number.isFinite(Number(text))
    case "duration":
      return parseGoDuration(promptDuration(text)) !== null
    case "datetime":
      return !Number.isNaN(Date.parse(text))
  }
  return true
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

/**
 * A duration as the prompt takes it: a Go duration ("30s", "1m30s"), or a
 * bare number, in seconds ("5" is "5s", "1.5" is "1.5s"). What goes to core
 * is the Go duration.
 */
export function promptDuration(text: string): string {
  return /^\d+(\.\d+)?$/.test(text.trim()) ? `${text.trim()}s` : text
}

/** A param's value from its text, in its type. */
function convert(
  param: CommandParam,
  text: string,
  connections: readonly ConnectionChoice[],
): { ok: true; value: unknown } | { ok: false; error: string } {
  if (param.format === CONNECTION_FORMAT) return connectionFromText(param.key, text, connections)
  switch (param.type) {
    case "enum":
      if (param.options?.length && !param.options.includes(text)) return { ok: false, error: `${param.key}: one of ${param.options.join(", ")}` }
      return { ok: true, value: text }
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
    case "duration":
      return { ok: true, value: promptDuration(text) }
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
  // Text, a datetime (RFC 3339): as typed.
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
export function lineTexts(line: string, commands: readonly CommandDef[], connections: readonly ConnectionChoice[] = []): LineTexts {
  const command = commandOf(line, commands)
  if (!command) return { texts: new Map(), extra: [] }
  const { texts, extra } = readWords(command, line, words(line).slice(1), connections)
  // A duration typed in seconds opens the form as the duration it is.
  for (const p of command.params) {
    const t = texts.get(p.key)
    if (p.type === "duration" && t !== undefined) texts.set(p.key, promptDuration(t))
  }
  return { command, texts, extra }
}

interface WordsRead {
  texts: Map<string, string>
  extra: string[]
  /** The param each word went to, by its place (none: an option's, or extra). */
  at: (string | undefined)[]
}

/**
 * A command's words (past its own) as its params' texts. Options set what
 * they name; each other word goes to the next param, in order, that takes
 * it, the params before passed: one that does not apply as the line stands
 * takes none, an optional one takes one that is one of its values (fits)
 * when words are left for the required params after it, a required one
 * takes any, and the rest param (restParam) the rest of the line. A word
 * no param takes goes to one it did not fit, for its error to say why, or
 * else is extra.
 */
function readWords(command: CommandDef, line: string, ws: readonly Word[], connections: readonly ConnectionChoice[]): WordsRead {
  const params = command.params
  const byKey = new Map(params.map((p) => [p.key, p]))
  const texts = new Map<string, string>()
  const extra: string[] = []
  const rest = restParam(command)

  // An option: --key=value, or --key value. Only a param's key makes one.
  const option = (i: number): { key: string; value: string; used: number } | null => {
    const m = /^--([A-Za-z0-9_.-]+)(?:=(.*))?$/.exec(ws[i].text)
    if (!m || !byKey.has(m[1])) return null
    if (m[2] !== undefined) return { key: m[1], value: m[2], used: 1 }
    if (i + 1 < ws.length) return { key: m[1], value: ws[i + 1].text, used: 2 }
    return null
  }

  let cut = ws.length
  if (rest) {
    // The options at the end come first: what is before them may be the
    // rest param's text, options in it included.
    for (;;) {
      let found = false
      for (const back of [1, 2]) {
        const i = cut - back
        if (i < 0) continue
        const o = option(i)
        if (o && i + o.used === cut) {
          texts.set(o.key, o.value)
          cut = i
          found = true
          break
        }
      }
      if (!found) break
    }
  } else {
    // An option anywhere counts for the words before it too.
    for (let i = 0; i < cut; ) {
      const o = option(i)
      if (o) texts.set(o.key, o.value)
      i += o ? o.used : 1
    }
  }

  const open = (p: CommandParam) => !texts.has(p.key) && paramApplies(p, params, texts)
  // Words from i to the cut, options aside; required params from k on.
  const wordsLeft = (i: number) => {
    let n = 0
    for (let j = i; j < cut; ) {
      const o = option(j)
      if (!o) n++
      j += o ? o.used : 1
    }
    return n
  }
  const requiredLeft = (k: number) => params.slice(k).filter((p) => isRequired(p) && open(p)).length

  const at: (string | undefined)[] = []
  let next = 0
  let restTaken = false
  for (let i = 0; i < cut && !restTaken; i++) {
    const o = option(i)
    if (o) {
      texts.set(o.key, o.value)
      i += o.used - 1
      continue
    }
    const word = ws[i].text
    let misfit: CommandParam | undefined
    let took = false
    for (; next < params.length && !took; next++) {
      const p = params[next]
      if (!open(p)) continue
      if (p === rest) {
        // As typed; a text wholly in quotes unquoted.
        texts.set(p.key, i === cut - 1 ? word : line.slice(ws[i].start, ws[cut - 1].end).trim())
        for (let j = i; j < cut; j++) at[j] = p.key
        took = restTaken = true
      } else if (isRequired(p) || (fits(p, word, connections) && wordsLeft(i) > requiredLeft(next + 1))) {
        texts.set(p.key, word)
        at[i] = p.key
        took = true
      } else if (!misfit && !fits(p, word, connections)) {
        misfit = p
      }
    }
    if (took) continue
    if (misfit) {
      texts.set(misfit.key, word)
      at[i] = misfit.key
    } else extra.push(word)
  }
  return { texts, extra, at }
}

/** Reads a line as one of commands with its args; connections are the session's, for a connection param. */
export function parseCommandLine(
  line: string,
  commands: readonly CommandDef[],
  connections: readonly ConnectionChoice[] = [],
): ParsedLine {
  const all = words(line)
  if (all.length === 0) return { ok: false, error: "Type a command" }
  const { command, texts, extra } = lineTexts(line, commands, connections)
  if (!command) {
    return { ok: false, error: `No command "${all[0].text}": ${commands.map((c) => c.id).join(", ") || "none"}` }
  }
  if (extra.length > 0) {
    const why = command.params.length === 0 ? `${command.id} takes nothing` : `No param of ${command.id} takes "${extra[0]}"`
    return { ok: false, command, error: `${why}: ${usage(command)}` }
  }
  const byKey = new Map(command.params.map((p) => [p.key, p]))
  const args: Record<string, unknown> = {}
  for (const [key, text] of texts) {
    const r = convert(byKey.get(key)!, text, connections)
    if (!r.ok) return { ok: false, command, error: r.error }
    args[key] = r.value
  }
  const stray = command.params.find((p) => p.key in args && p.when && !paramApplies(p, command.params, args))
  if (stray?.when) return { ok: false, command, error: `${stray.key}: only when ${conditionText(stray.when)}` }
  const missing = command.params.filter((p) => isRequired(p) && !(p.key in args) && paramApplies(p, command.params, args))
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
  // The rest param's text first, as typed, where it is the first param (a
  // shell's command line, a path); else every value as an option, so a
  // word of it is never read as an optional param's value.
  const rest = def ? restParam(def) : undefined
  const restText = rest && def!.params[0] === rest && args ? args[rest.key] : undefined
  if (rest && typeof restText === "string" && restText.trim() !== "" && !restText.trimStart().startsWith("--")) {
    parts.push(restText)
    for (const [k, v] of entries) if (k !== rest.key) parts.push(`--${k}=${quote(show(v))}`)
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
  if (!command) return null
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

  // The word goes where the line read so far puts it.
  const read = readWords(command, line, [...before, current], connections)
  return offer(byKey.get(read.at[before.length] ?? ""), current.start, current.text)
}

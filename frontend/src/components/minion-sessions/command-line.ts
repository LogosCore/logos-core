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
// type here (a number, a list split at commas); core checks them again.

export interface CommandParam {
  key: string
  type: string
  label: string
  required: boolean
  default?: unknown
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
function convert(param: CommandParam, text: string): { ok: true; value: unknown } | { ok: false; error: string } {
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

/** Reads a line as one of commands with its args. */
export function parseCommandLine(line: string, commands: readonly CommandDef[]): ParsedLine {
  const all = words(line)
  if (all.length === 0) return { ok: false, error: "Type a command" }
  const command = commandOf(line, commands)
  if (!command) {
    return { ok: false, error: `No command "${all[0].text}": ${commands.map((c) => c.id).join(", ") || "none"}` }
  }
  const byKey = new Map(command.params.map((p) => [p.key, p]))
  const texts = new Map<string, string>()
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
    if (positional.length > free.length) {
      return { ok: false, command, error: `${command.id} takes ${command.params.length === 0 ? "nothing" : `at most ${command.params.length}`}: ${usage(command)}` }
    }
    positional.forEach((text, i) => texts.set(free[i].key, text))
  }

  const args: Record<string, unknown> = {}
  for (const [key, text] of texts) {
    const r = convert(byKey.get(key)!, text)
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

/** A task as a line again: its command and args, as typed. */
export function lineOf(command: string, args: Record<string, unknown> | null | undefined, def?: CommandDef): string {
  const parts = [command]
  const entries = Object.entries(args ?? {})
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

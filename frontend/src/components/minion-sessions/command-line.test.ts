import { describe, expect, test } from "vitest"
import {
  commandHistory,
  commandOf,
  conditionText,
  lineOf,
  lineTexts,
  parseCommandLine,
  restParam,
  usage,
  valueCompletion,
  words,
  type CommandDef,
} from "./command-line"

const commands: CommandDef[] = [
  {
    id: "shell",
    label: "Run a shell command",
    params: [
      { key: "command", type: "string", label: "Command", required: true },
      { key: "timeout", type: "duration", label: "Timeout", required: false, default: "30s" },
    ],
  },
  { id: "ls", label: "List a directory", params: [{ key: "path", type: "string", label: "Path", required: false, default: "." }] },
  {
    id: "sleep",
    label: "Sleep",
    params: [
      { key: "interval", type: "duration", label: "Interval", required: true },
      { key: "jitter", type: "int", label: "Jitter", required: false, default: 10 },
    ],
  },
  { id: "exit", label: "Exit", params: [] },
]

describe("the console's command line", () => {
  test("a text first param takes the rest of the line verbatim; options only at its end", () => {
    expect(parseCommandLine("shell ls -la /tmp | wc -l", commands)).toMatchObject({ ok: true, args: { command: "ls -la /tmp | wc -l" } })
    expect(parseCommandLine("shell sleep 9 --timeout=1m", commands)).toMatchObject({ ok: true, args: { command: "sleep 9", timeout: "1m" } })
    expect(parseCommandLine("shell sleep 9 --timeout 1m", commands)).toMatchObject({ ok: true, args: { command: "sleep 9", timeout: "1m" } })
    // An option in the middle is the command's own.
    expect(parseCommandLine("shell grep --timeout=1 x", commands)).toMatchObject({ ok: true, args: { command: "grep --timeout=1 x" } })
    expect(parseCommandLine('ls "/my dir"', commands)).toMatchObject({ ok: true, args: { path: "/my dir" } })
    expect(parseCommandLine("ls", commands)).toMatchObject({ ok: true, args: {} })
  })

  test("positional words fill the params in order, options anywhere, in their types", () => {
    expect(parseCommandLine("sleep 30s 20", commands)).toMatchObject({ ok: true, args: { interval: "30s", jitter: 20 } })
    expect(parseCommandLine("sleep --jitter=5 30s", commands)).toMatchObject({ ok: true, args: { interval: "30s", jitter: 5 } })
    expect(parseCommandLine("SLEEP 1m", commands)).toMatchObject({ ok: true, command: { id: "sleep" } })
  })

  test("what does not read says why", () => {
    expect(parseCommandLine("", commands)).toMatchObject({ ok: false, error: "Type a command" })
    expect(parseCommandLine("rm -rf /", commands)).toMatchObject({ ok: false, error: expect.stringContaining('No command "rm"') })
    expect(parseCommandLine("sleep", commands)).toMatchObject({ ok: false, error: "Missing interval: sleep <interval> [jitter=10]" })
    expect(parseCommandLine("shell", commands)).toMatchObject({ ok: false, error: expect.stringContaining("Missing command") })
    expect(parseCommandLine("sleep 1s x", commands)).toMatchObject({ ok: false, error: "jitter: a whole number" })
    expect(parseCommandLine("exit now", commands)).toMatchObject({ ok: false, error: expect.stringContaining("takes nothing") })
  })

  test("words, the command typed, usage, and a task as a line again", () => {
    expect(words(`a "b c" d'e f'`).map((w) => w.text)).toEqual(["a", "b c", "de f"])
    expect(commandOf("  ls /", commands)?.id).toBe("ls")
    expect(usage(commands[0])).toBe("shell <command> [timeout=30s]")
    expect(lineOf("shell", { command: "ls -la", timeout: "1m" }, commands[0])).toBe("shell ls -la --timeout=1m")
    expect(lineOf("sleep", { interval: "30s", jitter: 20 }, commands[2])).toBe("sleep --interval=30s --jitter=20")
  })

  test("the history: each line once, where it was last run, the last of them", () => {
    expect(commandHistory(["ls", "shell id", "ls", "hello", "shell id"])).toEqual(["ls", "hello", "shell id"])
    expect(commandHistory(["a", "b", "c"], 2)).toEqual(["b", "c"])
    expect(commandHistory([])).toEqual([])
  })

  test("a line read leniently for the form: each param's text as typed, nothing checked", () => {
    const t = lineTexts("sleep abc --jitter=x extra", commands)
    expect(t.command?.id).toBe("sleep")
    expect(Object.fromEntries(t.texts)).toEqual({ interval: "abc", jitter: "x" })
    expect(t.extra).toEqual(["extra"])
    expect(Object.fromEntries(lineTexts("shell ls -la --timeout=1m", commands).texts)).toEqual({ command: "ls -la", timeout: "1m" })
    expect(lineTexts("sleep", commands).texts.size).toBe(0)
    expect(lineTexts("rm -rf /", commands).command).toBeUndefined()
  })
})

describe("params by a condition", () => {
  // As core offers a tunnel: the factory's own params first, then core's.
  const forward = { key: "front", values: ["forward"] }
  const tunnel: CommandDef = {
    id: "tunnel",
    label: "Tunnel",
    params: [
      { key: "timeout", type: "duration", label: "Connect timeout", required: false, default: "10s", when: { key: "front", values: ["socks", "forward"] } },
      { key: "front", type: "enum", label: "Front", required: false, default: "socks" },
      { key: "port", type: "int", label: "Port", required: false, default: 0 },
      { key: "host", type: "string", label: "Destination", required: true, when: forward },
      { key: "to", type: "int", label: "Destination port", required: false, when: forward },
      { key: "program", type: "string", label: "Program", required: false, when: { key: "front", values: ["terminal"] } },
    ],
  }
  const cmds = [tunnel]

  test("positional words skip the params that do not apply as the line stands", () => {
    expect(parseCommandLine("tunnel 5s forward 8080 db.local 5432", cmds)).toMatchObject({
      ok: true,
      args: { timeout: "5s", front: "forward", port: 8080, host: "db.local", to: 5432 },
    })
    expect(parseCommandLine("tunnel --front=terminal 2222 bash", cmds)).toEqual({
      ok: true,
      command: tunnel,
      args: { front: "terminal", port: 2222, program: "bash" },
    })
  })

  test("a param given where it does not apply is refused, as core would; one required is so only where it applies", () => {
    expect(parseCommandLine("tunnel --host=db.local", cmds)).toMatchObject({ ok: false, error: "host: only when front is forward" })
    expect(parseCommandLine("tunnel --timeout=5s --front=terminal", cmds)).toMatchObject({ ok: false, error: "timeout: only when front is socks or forward" })
    // A word no param takes goes to one it did not fit, which says why.
    expect(parseCommandLine("tunnel --front=forward 8080 db.local x", cmds)).toMatchObject({ ok: false, error: "to: a whole number" })
    expect(parseCommandLine("tunnel", cmds)).toMatchObject({ ok: true, args: {} })
    expect(parseCommandLine("tunnel --front=forward", cmds)).toMatchObject({ ok: false, error: expect.stringContaining("Missing host") })
  })

  test("a condition reads as core says it", () => {
    expect(conditionText(forward)).toBe("front is forward")
    expect(conditionText({ key: "front", values: ["socks", "http", "forward"] })).toBe("front is socks, http or forward")
  })
})

describe("words in the form's order", () => {
  const feedback: CommandDef = {
    id: "feedback",
    label: "Send feedback",
    params: [
      { key: "level", type: "enum", label: "Level", required: false, default: "warning", options: ["warning", "error"] },
      { key: "message", type: "string", label: "Message", required: true },
    ],
  }
  // Core's params in the form's order: where the minion listens, the
  // destination, then More options.
  const reverse: CommandDef = {
    id: "reverse_tunnel",
    label: "Reverse tunnel",
    params: [
      { key: "bind", type: "string", label: "Listen address", required: false, default: "0.0.0.0" },
      { key: "listen", type: "int", label: "Listen port", required: true },
      { key: "udp", type: "bool", label: "UDP", required: false, default: false },
      { key: "host", type: "string", label: "Destination", required: true },
      { key: "to", type: "int", label: "Destination port", required: true },
      { key: "timeout", type: "duration", label: "Connect timeout", required: false, default: "10s" },
    ],
  }
  const terminal = { key: "front", values: ["terminal"] }
  const forward = { key: "front", values: ["forward"] }
  const tunnel: CommandDef = {
    id: "tunnel",
    label: "Tunnel",
    params: [
      { key: "front", type: "enum", label: "Front", required: false, default: "socks", options: ["socks", "http", "forward", "terminal"] },
      { key: "port", type: "int", label: "Port", required: false, default: 0 },
      { key: "host", type: "string", label: "Destination", required: false, when: forward },
      { key: "to", type: "int", label: "Destination port", required: false, when: forward },
      { key: "program", type: "string", label: "Program", required: false, when: terminal },
      { key: "args", type: "string", label: "Args", required: false, when: terminal },
      { key: "timeout", type: "duration", label: "Connect timeout", required: false, default: "10s", when: { key: "front", values: ["socks", "http", "forward"] } },
    ],
  }
  const help: CommandDef = {
    id: "help",
    label: "Help",
    params: [{ key: "command", type: "enum", label: "Command", required: false, options: ["feedback", "help"] }],
  }
  const cmds = [feedback, reverse, tunnel, help, ...commands]

  test("an optional param is passed over by a word that is none of its values, or that a required one needs", () => {
    expect(parseCommandLine("feedback asdf", cmds)).toMatchObject({ ok: true, args: { message: "asdf" } })
    expect(parseCommandLine("feedback disk is full", cmds)).toMatchObject({ ok: true, args: { message: "disk is full" } })
    expect(parseCommandLine("feedback error disk is full", cmds)).toMatchObject({ ok: true, args: { level: "error", message: "disk is full" } })
    // One word is the message's: the level would leave it none.
    expect(parseCommandLine("feedback error", cmds)).toEqual({ ok: true, command: feedback, args: { message: "error" } })
    expect(parseCommandLine("feedback --level=error disk", cmds)).toMatchObject({ ok: true, args: { level: "error", message: "disk" } })
    expect(parseCommandLine("feedback disk --level=error", cmds)).toMatchObject({ ok: true, args: { level: "error", message: "disk" } })
    expect(parseCommandLine("feedback", cmds)).toMatchObject({ ok: false, error: "Missing message: feedback [level=warning] <message>" })
  })

  test("words left for the required params pass the optional ones before them", () => {
    expect(parseCommandLine("reverse_tunnel 8080 10.8.0.2 4444", cmds)).toEqual({
      ok: true,
      command: reverse,
      args: { listen: 8080, host: "10.8.0.2", to: 4444 },
    })
    expect(parseCommandLine("reverse_tunnel 127.0.0.1 8080 10.8.0.2 4444", cmds)).toMatchObject({
      ok: true,
      args: { bind: "127.0.0.1", listen: 8080, host: "10.8.0.2", to: 4444 },
    })
    expect(parseCommandLine("reverse_tunnel 0.0.0.0 8080 true 10.8.0.2 4444 30s", cmds)).toMatchObject({
      ok: true,
      args: { bind: "0.0.0.0", listen: 8080, udp: true, host: "10.8.0.2", to: 4444, timeout: "30s" },
    })
    // No param of it is text to the end of the line: the host is one word.
    expect(restParam(reverse)).toBeUndefined()
  })

  test("a tunnel's words go to its front and port, then what the front takes", () => {
    expect(parseCommandLine("tunnel 1080", cmds)).toEqual({ ok: true, command: tunnel, args: { port: 1080 } })
    expect(parseCommandLine("tunnel http 3128", cmds)).toMatchObject({ ok: true, args: { front: "http", port: 3128 } })
    expect(parseCommandLine("tunnel forward 8080 db.local 5432", cmds)).toMatchObject({
      ok: true,
      args: { front: "forward", port: 8080, host: "db.local", to: 5432 },
    })
    expect(parseCommandLine("tunnel terminal 2222 bash -l", cmds)).toEqual({
      ok: true,
      command: tunnel,
      args: { front: "terminal", port: 2222, program: "bash", args: "-l" },
    })
  })

  test("a word for none of a command's params says so, or what it is not", () => {
    expect(parseCommandLine("help nonexistent", cmds)).toMatchObject({ ok: false, error: "command: one of feedback, help" })
    expect(parseCommandLine("tunnel socks 1080 x y", cmds)).toMatchObject({ ok: false, error: expect.stringContaining('No param of tunnel takes "y"') })
  })

  test("a task's line reads back as its args", () => {
    for (const [def, args] of [
      [feedback, { level: "warning", message: "error in the disk" }],
      [feedback, { message: "error in the disk" }],
      [commands[0], { command: "ls -la /tmp", timeout: "1m" }],
      [reverse, { bind: "0.0.0.0", listen: 8080, udp: false, host: "10.8.0.2", to: 4444 }],
    ] as const) {
      const line = lineOf(def.id, args, def)
      expect(parseCommandLine(line, cmds)).toMatchObject({ ok: true, args })
    }
    expect(lineOf("feedback", { level: "warning", message: "disk full" }, feedback)).toBe('feedback --level=warning --message="disk full"')
  })

  test("a connection param's completion follows the same words", () => {
    const conns = [{ place: 1, name: "office", kind: "reverse-call", address: "https://a", inUse: true }] as unknown as Parameters<typeof valueCompletion>[2]
    const switchCmd: CommandDef = {
      id: "switch",
      label: "Switch",
      params: [
        { key: "mode", type: "enum", label: "Mode", required: false, options: ["now", "later"] },
        { key: "conn", type: "int", label: "Connection", required: true, format: "connection" },
      ],
    }
    expect(valueCompletion("switch off", [switchCmd], conns)).toMatchObject({ key: "conn", partial: "off" })
    expect(valueCompletion("switch now off", [switchCmd], conns)).toMatchObject({ key: "conn", partial: "off" })
    expect(valueCompletion("feedback off", cmds, conns)).toBeNull()
  })
})

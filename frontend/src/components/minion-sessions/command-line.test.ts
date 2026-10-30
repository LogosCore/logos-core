import { describe, expect, test } from "vitest"
import { commandOf, lineOf, parseCommandLine, usage, words, type CommandDef } from "./command-line"

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
})

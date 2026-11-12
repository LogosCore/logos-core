import { describe, expect, test } from "vitest"
import { lineOf, type CommandDef } from "./command-line"

// Core keeps each task's line as a search text (models.MinionTaskSearchText),
// for the task list's search to find what a row shows. Its cases, in core's
// testdata, are the line lineOf shows for each.

interface TaskLine {
  why: string
  command: string
  /** The param shown bare after the command; "" for none. */
  rest: string
  args: Record<string, unknown>
  line: string
}

/** A value's keys sorted, at any depth: as core's JSON sends them. */
function sorted(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(sorted)
  if (v && typeof v === "object") {
    return Object.fromEntries(
      Object.entries(v)
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        .map(([k, x]) => [k, sorted(x)]),
    )
  }
  return v
}

describe("a task's line, as core keeps it for the search", () => {
  test("is the one the list shows", async () => {
    const { readFileSync } = await import("node:fs")
    const { resolve } = await import("node:path")
    const cases = JSON.parse(readFileSync(resolve(__dirname, "../../../../core/pkg/models/testdata/minion-task-lines.json"), "utf8")) as TaskLine[]
    expect(cases.length).toBeGreaterThan(5)
    for (const c of cases) {
      // A command whose first param takes the rest of a line: free text, the
      // first, none required before it.
      const def: CommandDef | undefined = c.rest
        ? {
            id: c.command,
            label: c.command,
            params: [{ key: c.rest, type: "string", label: c.rest, required: false, default: null }],
          }
        : undefined
      expect(lineOf(c.command, sorted(c.args) as Record<string, unknown>, def), c.why).toBe(c.line)
    }
  })
})

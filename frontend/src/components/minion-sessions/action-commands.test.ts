import { describe, expect, test } from "vitest"
import { actionCommands, chooseActionCommand, commandsFor } from "./action-commands"

// find shows a listing (its result) but is no list_dir: it is never run for one.
const commands = [
  { id: "sleep", action: null },
  { id: "ls_sh", action: "list_dir" },
  { id: "find", action: null },
  { id: "ls", action: "list_dir" },
]

describe("the command run for an action", () => {
  test("the session's commands with the action, in the factory's order", () => {
    expect(actionCommands(commands, "list_dir").map((c) => c.id)).toEqual(["ls_sh", "ls"])
    expect(actionCommands(commands, "download_file")).toEqual([])
  })

  test("the operator's pick while the session has it, else the factory's first", () => {
    const listing = actionCommands(commands, "list_dir")
    expect(chooseActionCommand(listing, null)?.id).toBe("ls_sh")
    expect(chooseActionCommand(listing, "ls")?.id).toBe("ls")
    expect(chooseActionCommand(listing, "gone")?.id).toBe("ls_sh")
    expect(chooseActionCommand([], "ls")).toBeUndefined()
  })

  test("an action on a view's item: the view's own command first, when it has the action", () => {
    expect(commandsFor(commands, "list_dir", "ls").map((c) => c.id)).toEqual(["ls", "ls_sh"])
    expect(commandsFor(commands, "list_dir").map((c) => c.id)).toEqual(["ls_sh", "ls"])
    // A find listing: its own command is no list_dir.
    expect(commandsFor(commands, "list_dir", "find").map((c) => c.id)).toEqual(["ls_sh", "ls"])
  })
})

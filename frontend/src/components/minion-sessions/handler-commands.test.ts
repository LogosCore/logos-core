import { describe, expect, test } from "vitest"
import { chooseHandlerCommand, handlerCommands } from "./handler-commands"

const commands = [
  { id: "sleep", handler: null },
  { id: "ls_sh", handler: "dir_listing" },
  { id: "ls", handler: "dir_listing" },
  { id: "ps", handler: "process_list" },
]

describe("the command run for a handler", () => {
  test("the session's commands with the handler, in the factory's order", () => {
    expect(handlerCommands(commands, "dir_listing").map((c) => c.id)).toEqual(["ls_sh", "ls"])
    expect(handlerCommands(commands, "screenshot")).toEqual([])
  })

  test("the operator's pick while the session has it, else the factory's first", () => {
    const listing = handlerCommands(commands, "dir_listing")
    expect(chooseHandlerCommand(listing, null)?.id).toBe("ls_sh")
    expect(chooseHandlerCommand(listing, "ls")?.id).toBe("ls")
    expect(chooseHandlerCommand(listing, "gone")?.id).toBe("ls_sh")
    expect(chooseHandlerCommand([], "ls")).toBeUndefined()
  })
})

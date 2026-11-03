import { describe, expect, test } from "vitest"
import { commandKindsNote, kindsList } from "./command-kinds"

describe("command kinds", () => {
  test("a command for every kind says nothing", () => {
    expect(commandKindsNote([], "reverse-call")).toBeNull()
    expect(commandKindsNote(undefined)).toBeNull()
  })
  test("where it works, and on another kind that the minion is switched first", () => {
    const kinds = ["reverse-persistent", "bind-persistent"]
    expect(kindsList(kinds)).toBe("reverse-persistent or bind-persistent")
    expect(kindsList(["a", "b", "c"])).toBe("a, b or c")
    expect(commandKindsNote(kinds, "bind-persistent")).toEqual({
      text: "Over reverse-persistent or bind-persistent connections only",
      blocked: false,
    })
    expect(commandKindsNote(kinds, null)?.blocked).toBe(false)
    expect(commandKindsNote(kinds, "reverse-call")).toEqual({
      text: "Over reverse-persistent or bind-persistent connections only: this minion uses reverse-call, switch it first",
      blocked: true,
    })
  })
})

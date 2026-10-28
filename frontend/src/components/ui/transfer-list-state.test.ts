import { describe, expect, it } from "vitest"
import {
  after,
  clicked,
  extended,
  grouped,
  leaving,
  matches,
  moved,
  NO_MARKS,
  normalized,
  sideItems,
  stepped,
  type TransferItem,
} from "./transfer-list-state"

const items: TransferItem[] = [
  { id: "sleep", label: "Sleep", hint: "sleep", group: "Session", locked: true },
  { id: "exit", label: "Exit", hint: "exit", group: "Session", locked: true },
  { id: "hello", label: "Say hello", hint: "hello", description: "Answers with the greeting.", group: "Session" },
  { id: "ls", label: "List a directory", hint: "ls", group: "Files" },
  { id: "download", label: "Download a file", hint: "download", group: "Files" },
  { id: "shell", label: "Run a shell command", hint: "shell", group: "Execution" },
]
const ids = (list: readonly TransferItem[]) => list.map((i) => i.id)

describe("the lists", () => {
  it("split the items by the selection, the locked ones selected whatever it says, in the items' order", () => {
    const selected = ["shell", "hello"]
    expect(ids(sideItems(items, selected, "selected", ""))).toEqual(["sleep", "exit", "hello", "shell"])
    expect(ids(sideItems(items, selected, "available", ""))).toEqual(["ls", "download"])
    expect(normalized(items, selected)).toEqual(["sleep", "exit", "hello", "shell"])
  })

  it("show what matches a search, by label, hint or description, case aside", () => {
    expect(matches(items[2], "GREETING")).toBe(true)
    expect(matches(items[3], "ls")).toBe(true)
    expect(matches(items[3], "file")).toBe(false)
    expect(ids(sideItems(items, [], "available", "file"))).toEqual(["download"])
    expect(ids(sideItems(items, [], "available", "  "))).toEqual(["hello", "ls", "download", "shell"])
  })

  it("keep the items under their headings, in the order the headings first come", () => {
    expect(grouped(sideItems(items, ["shell"], "selected", "")).map((g) => [g.group, ids(g.items)])).toEqual([
      ["Session", ["sleep", "exit"]],
      ["Execution", ["shell"]],
    ])
  })
})

describe("moving", () => {
  it("adds to the selection, or takes from it, the locked items staying", () => {
    expect(moved(items, ["hello"], ["shell", "ls"], "selected")).toEqual(["sleep", "exit", "hello", "ls", "shell"])
    expect(moved(items, ["hello", "ls"], ["sleep", "hello"], "available")).toEqual(["sleep", "exit", "ls"])
  })

  it("moves the marked items a list shows and can let go, or else the focused one", () => {
    const selected = sideItems(items, ["hello", "ls"], "selected", "")
    const marks = { marked: new Set(["sleep", "hello", "shell"]), anchor: null }
    expect(leaving(selected, "selected", marks, "ls")).toEqual(["hello"]) // sleep is locked, shell not shown
    expect(leaving(selected, "selected", NO_MARKS, "ls")).toEqual(["ls"])
    expect(leaving(selected, "selected", NO_MARKS, "exit")).toEqual([])
    expect(leaving(selected, "selected", NO_MARKS, null)).toEqual([])
  })

  it("leaves focus on the item after the ones gone, or the one before", () => {
    const shown = ["a", "b", "c", "d"]
    expect(after(shown, ["b", "c"])).toBe("d")
    expect(after(shown, ["c", "d"])).toBe("b")
    expect(after(shown, shown)).toBeNull()
    expect(after(shown, ["x"])).toBeNull()
  })
})

describe("marking", () => {
  const shown = ["a", "b", "c", "d", "e"]

  it("marks a clicked item alone, toggles one with Ctrl, and spans from the anchor with Shift", () => {
    let marks = clicked(shown, NO_MARKS, "b", { shift: false, toggle: false })
    expect([...marks.marked]).toEqual(["b"])
    marks = clicked(shown, marks, "d", { shift: true, toggle: false })
    expect([...marks.marked]).toEqual(["b", "c", "d"])
    expect(marks.anchor).toBe("b")
    marks = clicked(shown, marks, "c", { shift: false, toggle: true })
    expect([...marks.marked].sort()).toEqual(["b", "d"])
    marks = clicked(shown, marks, "a", { shift: true, toggle: false }) // from the new anchor, c
    expect([...marks.marked]).toEqual(["a", "b", "c"])
  })

  it("spans from the clicked item when the anchor is no longer shown", () => {
    const marks = clicked(shown, { marked: new Set(["x"]), anchor: "x" }, "c", { shift: true, toggle: false })
    expect([...marks.marked]).toEqual(["c"])
  })

  it("moves keyboard focus within the items shown", () => {
    expect(stepped(shown, null, "next")).toBe("a")
    expect(stepped(shown, "b", "next")).toBe("c")
    expect(stepped(shown, "e", "next")).toBe("e")
    expect(stepped(shown, "a", "previous")).toBe("a")
    expect(stepped(shown, "c", "last")).toBe("e")
    expect(stepped(shown, "c", "first")).toBe("a")
    expect(stepped([], "c", "next")).toBeNull()
  })

  it("extends the marks with Shift as focus moves, from where it was", () => {
    expect(extended(shown, NO_MARKS, "b", "c", false)).toBe(NO_MARKS)
    const marks = extended(shown, NO_MARKS, "b", "d", true)
    expect([...marks.marked]).toEqual(["b", "c", "d"])
    expect([...extended(shown, marks, "d", "a", true).marked]).toEqual(["a", "b"])
  })
})

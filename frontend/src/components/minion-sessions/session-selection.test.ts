import { describe, expect, test } from "vitest"
import { emptySelection, selectLoaded, selectedIds, selectionIn, toggleSelected } from "./session-selection"

const order = ["a", "b", "c", "d", "e"]

describe("session selection", () => {
  test("a click toggles a row; shift-click selects the range from the last click", () => {
    let s = toggleSelected(emptySelection("x"), order, "b", false)
    expect(selectedIds(s, order)).toEqual(["b"])
    s = toggleSelected(s, order, "e", true)
    expect(selectedIds(s, order)).toEqual(["b", "c", "d", "e"])
    s = toggleSelected(s, order, "c", false)
    expect(selectedIds(s, order)).toEqual(["b", "d", "e"])
    // Backwards from the last click.
    s = toggleSelected(s, order, "a", true)
    expect(selectedIds(s, order)).toEqual(["a", "b", "c", "d", "e"])
  })

  test("a shift-click with nothing clicked before toggles the row alone", () => {
    expect(selectedIds(toggleSelected(emptySelection("x"), order, "c", true), order)).toEqual(["c"])
  })

  test("all matching takes every loaded row; a click then goes back to picked rows", () => {
    const all = { ...emptySelection("x"), all: true }
    expect(selectedIds(all, order)).toEqual(order)
    const s = toggleSelected(all, order, "c", false)
    expect(s.all).toBe(false)
    expect(selectedIds(s, order)).toEqual(["a", "b", "d", "e"])
  })

  test("rows that leave the list drop out; another scope is empty", () => {
    const s = selectLoaded(emptySelection("x"), order, true)
    expect(selectedIds(s, ["a", "c"])).toEqual(["a", "c"])
    expect(selectionIn(s, "x")).toBe(s)
    expect(selectedIds(selectionIn(s, "y"), order)).toEqual([])
    expect(selectedIds(selectLoaded(s, order, false), order)).toEqual([])
  })
})

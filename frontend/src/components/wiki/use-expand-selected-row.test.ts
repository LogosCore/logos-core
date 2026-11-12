import { describe, expect, test } from "vitest"
import { expandsNow } from "./use-expand-selected-row"

describe("the open page expanded", () => {
  test("a page with children expands when it opens", () => {
    expect(expandsNow(null, { id: "p", hadChildren: true })).toBe(true)
    expect(expandsNow({ id: "q", hadChildren: true }, { id: "p", hadChildren: true })).toBe(true)
  })

  test("a page without children is left alone", () => {
    expect(expandsNow(null, { id: "p", hadChildren: false })).toBe(false)
  })

  test("the open page gaining its first child expands", () => {
    expect(expandsNow({ id: "p", hadChildren: false }, { id: "p", hadChildren: true })).toBe(true)
  })

  // A reader who collapsed the open page keeps it collapsed.
  test("the open page that had children does not expand again", () => {
    expect(expandsNow({ id: "p", hadChildren: true }, { id: "p", hadChildren: true })).toBe(false)
  })
})

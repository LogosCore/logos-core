import { beforeEach, describe, expect, test, vi } from "vitest"

const memory = new Map<string, string>()
vi.stubGlobal("localStorage", {
  getItem: (k: string) => memory.get(k) ?? null,
  setItem: (k: string, v: string) => void memory.set(k, v),
  removeItem: (k: string) => void memory.delete(k),
})

const { readStylePreference, styleOf, writeStylePreference } = await import("./drawing-style-preference")

describe("the drawing style preference", () => {
  beforeEach(() => memory.clear())

  test("is nothing until something is remembered", () => {
    expect(readStylePreference()).toEqual({})
  })

  test("remembers the style fields of an appState, and only those", () => {
    const style = styleOf({
      currentItemArrowType: "elbow",
      currentItemStrokeColor: "#1971c2",
      currentItemRoughness: 0,
      currentItemEndArrowhead: null,
      zoom: { value: 2 },
      gridModeEnabled: true,
    } as never)
    expect(style).toEqual({
      currentItemArrowType: "elbow",
      currentItemStrokeColor: "#1971c2",
      currentItemRoughness: 0,
      currentItemEndArrowhead: null,
    })
    writeStylePreference(style)
    expect(readStylePreference()).toEqual(style)
  })

  test("drops values that are not valid, keeping the rest", () => {
    localStorage.setItem(
      "wiki_drawing_style",
      JSON.stringify({ currentItemArrowType: "wavy", currentItemStrokeWidth: -1, currentItemFillStyle: "solid", other: 1 }),
    )
    expect(readStylePreference()).toEqual({ currentItemFillStyle: "solid" })
  })

  test("survives a corrupt entry", () => {
    localStorage.setItem("wiki_drawing_style", "{not json")
    expect(readStylePreference()).toEqual({})
  })
})

import { describe, expect, test } from "vitest"
import { NO_TABS, activateTab, closeTabs, moveTab, openTab, readTabs, tabsToClose } from "./work-pane-tabs"

const tabs = (active: string | null, ...ids: string[]) => ({ ids, active })

describe("work pane tabs", () => {
  test("opening adds a tab at the end in front; an open one only comes to the front", () => {
    let t = openTab(NO_TABS, "a")
    t = openTab(t, "b")
    expect(t).toEqual(tabs("b", "a", "b"))
    expect(openTab(t, "a")).toEqual(tabs("a", "a", "b"))
    expect(openTab(t, "b")).toBe(t)
    expect(activateTab(t, "a")).toEqual(tabs("a", "a", "b"))
    expect(activateTab(t, "zz")).toBe(t)
  })

  test("closing the tab in front brings its right neighbour, else its left", () => {
    expect(closeTabs(tabs("b", "a", "b", "c"), ["b"])).toEqual(tabs("c", "a", "c"))
    expect(closeTabs(tabs("c", "a", "b", "c"), ["c"])).toEqual(tabs("b", "a", "b"))
    // One behind closes: the front stays.
    expect(closeTabs(tabs("a", "a", "b", "c"), ["c"])).toEqual(tabs("a", "a", "b"))
    // Several, the front among them: the nearest kept.
    expect(closeTabs(tabs("b", "a", "b", "c", "d"), ["b", "c"])).toEqual(tabs("d", "a", "d"))
    expect(closeTabs(tabs("a", "a"), ["a"])).toEqual(NO_TABS)
  })

  test("a tab's menu closes it, the others, those to its right, or all", () => {
    const t = tabs("a", "a", "b", "c")
    expect(tabsToClose(t, "b", "this")).toEqual(["b"])
    expect(tabsToClose(t, "b", "others")).toEqual(["a", "c"])
    expect(tabsToClose(t, "b", "right")).toEqual(["c"])
    expect(tabsToClose(t, "b", "all")).toEqual(["a", "b", "c"])
    expect(closeTabs(t, tabsToClose(t, "b", "others"))).toEqual(tabs("b", "b"))
  })

  test("a drag moves a tab to where another is", () => {
    expect(moveTab(tabs("a", "a", "b", "c"), "a", "c").ids).toEqual(["b", "c", "a"])
    expect(moveTab(tabs("a", "a", "b", "c"), "c", "a").ids).toEqual(["c", "a", "b"])
    const t = tabs("a", "a", "b")
    expect(moveTab(t, "a", "a")).toBe(t)
  })

  test("stored tabs are read back, anything else dropped", () => {
    expect(readTabs({ ids: ["a", "b"], active: "b" })).toEqual(tabs("b", "a", "b"))
    expect(readTabs({ ids: ["a", 3, "a", ""], active: "gone" })).toEqual(tabs("a", "a"))
    expect(readTabs(null)).toEqual(NO_TABS)
    expect(readTabs({ ids: "a" })).toEqual(NO_TABS)
  })
})

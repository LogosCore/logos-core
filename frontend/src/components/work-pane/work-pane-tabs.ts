// The tabs of a work pane (work-pane.tsx), as data: which are open, in
// what order, and which is in front. Pure, so the rules are tested apart
// from the component; the pane's owner keeps the state.

export interface WorkPaneTabs {
  /** The open tabs' ids, in their order. */
  ids: string[]
  /** The tab in front; null with none open. */
  active: string | null
}

export const NO_TABS: WorkPaneTabs = { ids: [], active: null }

/** Opens a tab (at the end) and brings it to the front; one already open only comes to the front. */
export function openTab(tabs: WorkPaneTabs, id: string): WorkPaneTabs {
  if (tabs.ids.includes(id)) return tabs.active === id ? tabs : { ...tabs, active: id }
  return { ids: [...tabs.ids, id], active: id }
}

/** Brings an open tab to the front. */
export function activateTab(tabs: WorkPaneTabs, id: string): WorkPaneTabs {
  return tabs.ids.includes(id) && tabs.active !== id ? { ...tabs, active: id } : tabs
}

/**
 * Closes tabs. When the one in front goes, the nearest kept to its right
 * comes to the front, else to its left, as browsers do.
 */
export function closeTabs(tabs: WorkPaneTabs, closing: readonly string[]): WorkPaneTabs {
  const gone = new Set(closing)
  const ids = tabs.ids.filter((id) => !gone.has(id))
  if (ids.length === tabs.ids.length) return tabs
  let active = tabs.active
  if (active === null || gone.has(active)) {
    const at = active === null ? -1 : tabs.ids.indexOf(active)
    const right = tabs.ids.slice(at + 1).find((id) => !gone.has(id))
    const left = tabs.ids.slice(0, Math.max(at, 0)).reverse().find((id) => !gone.has(id))
    active = right ?? left ?? null
  }
  return { ids, active }
}

/** The tabs a tab's menu closes: just it, the others, those to its right, or all. */
export function tabsToClose(tabs: WorkPaneTabs, id: string, which: "this" | "others" | "right" | "all"): string[] {
  switch (which) {
    case "this":
      return [id]
    case "others":
      return tabs.ids.filter((t) => t !== id)
    case "right":
      return tabs.ids.slice(tabs.ids.indexOf(id) + 1)
    case "all":
      return [...tabs.ids]
  }
}

/** Moves a tab to where another is (a drag). */
export function moveTab(tabs: WorkPaneTabs, id: string, to: string): WorkPaneTabs {
  const from = tabs.ids.indexOf(id)
  const at = tabs.ids.indexOf(to)
  if (from < 0 || at < 0 || from === at) return tabs
  const ids = [...tabs.ids]
  ids.splice(from, 1)
  ids.splice(at, 0, id)
  return { ...tabs, ids }
}

/** Tabs as stored, or none: whatever does not read as tabs is dropped. */
export function readTabs(stored: unknown): WorkPaneTabs {
  if (typeof stored !== "object" || stored === null) return NO_TABS
  const { ids, active } = stored as Record<string, unknown>
  if (!Array.isArray(ids)) return NO_TABS
  const kept = [...new Set(ids.filter((id): id is string => typeof id === "string" && id !== ""))]
  return { ids: kept, active: typeof active === "string" && kept.includes(active) ? active : (kept[0] ?? null) }
}

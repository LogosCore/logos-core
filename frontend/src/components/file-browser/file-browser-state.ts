import { useCallback, useSyncExternalStore } from "react"

// A file browser tab's state (docs: contracts/file-browser.md, The
// browser): the folder shown, the session its actions run through, Show
// deleted, the sort, the tree's open folders. Kept in this browser per tab,
// as the tabs are (use-session-tabs.ts), so a reload finds it as left; and
// a store, so a listing's "Open in File Browser" moves an open tab.

export interface FileBrowserState {
  /** The folder shown; null: the machine's roots. */
  path: string | null
  /** The session actions run through; null: the tab's own. */
  actingSessionId: string | null
  showDeleted: boolean
  sort: "name" | "size" | "modified"
  desc: boolean
  /** The tree's open folders. */
  expanded: string[]
  /** The tree's share of the width. */
  split: number
}

export const DEFAULT_STATE: FileBrowserState = {
  path: null,
  actingSessionId: null,
  showDeleted: true,
  sort: "name",
  desc: false,
  expanded: [],
  split: 0.25,
}

const SORTS = new Set(["name", "size", "modified"])
const key = (tabId: string) => `logos.fileBrowser.${tabId}`

/** The state as stored, each field checked: what does not read falls back. */
export function readState(stored: unknown): FileBrowserState {
  if (typeof stored !== "object" || stored === null) return DEFAULT_STATE
  const s = stored as Record<string, unknown>
  return {
    path: typeof s.path === "string" && s.path !== "" ? s.path : null,
    actingSessionId: typeof s.actingSessionId === "string" && s.actingSessionId !== "" ? s.actingSessionId : null,
    showDeleted: typeof s.showDeleted === "boolean" ? s.showDeleted : DEFAULT_STATE.showDeleted,
    sort: typeof s.sort === "string" && SORTS.has(s.sort) ? (s.sort as FileBrowserState["sort"]) : DEFAULT_STATE.sort,
    desc: s.desc === true,
    expanded: Array.isArray(s.expanded) ? s.expanded.filter((p): p is string => typeof p === "string").slice(0, 500) : [],
    split: typeof s.split === "number" && s.split > 0 && s.split < 1 ? s.split : DEFAULT_STATE.split,
  }
}

const cache = new Map<string, FileBrowserState>()
const listeners = new Set<() => void>()

function load(tabId: string): FileBrowserState {
  let s = cache.get(tabId)
  if (s) return s
  try {
    const raw = localStorage.getItem(key(tabId))
    s = raw ? readState(JSON.parse(raw)) : DEFAULT_STATE
  } catch {
    s = DEFAULT_STATE
  }
  cache.set(tabId, s)
  return s
}

/** Changes a tab's state, whether or not it is shown. */
export function setFileBrowserState(tabId: string, change: Partial<FileBrowserState>) {
  const next = { ...load(tabId), ...change }
  cache.set(tabId, next)
  try {
    localStorage.setItem(key(tabId), JSON.stringify(next))
  } catch {
    // Kept for the page only.
  }
  listeners.forEach((l) => l())
}

/** Forgets a closed tab's state. */
export function dropFileBrowserState(tabId: string) {
  cache.delete(tabId)
  try {
    localStorage.removeItem(key(tabId))
  } catch {
    // Nothing kept.
  }
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** A tab's state, and a setter of parts of it. */
export function useFileBrowserState(tabId: string): [FileBrowserState, (change: Partial<FileBrowserState>) => void] {
  const state = useSyncExternalStore(subscribe, () => load(tabId))
  const set = useCallback((change: Partial<FileBrowserState>) => setFileBrowserState(tabId, change), [tabId])
  return [state, set]
}

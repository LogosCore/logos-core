import { useCallback, useSyncExternalStore } from "react"
import { NO_TABS, openTab, readTabs, type WorkPaneTabs } from "@/components/work-pane/work-pane-tabs"

// The session tabs of an operation's work pane, kept in this browser: an
// operator's desk, not the team's. Each browser tab keeps its own
// (sessionStorage: a reload finds them as left, another browser tab has
// others); a new browser tab, or a later visit, starts from the last a tab
// left (localStorage). Storage can be missing or refuse (a private window):
// the tabs then last as long as the page. Read once, then kept here, so a
// tab opened from elsewhere (Interact, on a build's targets) reaches a
// Sessions page already showing under the modal.

const key = (operationId: string) => `logos.sessionTabs.${operationId}`

function read(storage: Storage, operationId: string): WorkPaneTabs | null {
  const raw = storage.getItem(key(operationId))
  return raw === null ? null : readTabs(JSON.parse(raw))
}

/** The tabs this browser tab had, else those the last one left. */
export function load(operationId: string): WorkPaneTabs {
  try {
    return read(sessionStorage, operationId) ?? read(localStorage, operationId) ?? NO_TABS
  } catch {
    return NO_TABS
  }
}

/** Keeps the tabs for this browser tab, and for the next one to open. */
export function save(operationId: string, tabs: WorkPaneTabs) {
  for (const storage of [sessionStorage, localStorage]) {
    try {
      // This tab's, even with none open: a reload keeps it empty, as left.
      if (tabs.ids.length === 0 && storage === localStorage) storage.removeItem(key(operationId))
      else storage.setItem(key(operationId), JSON.stringify(tabs))
    } catch {
      // Kept for the page only.
    }
  }
}

// The tabs as this page has them, by operation, and who shows them.
const kept = new Map<string, WorkPaneTabs>()
const listeners = new Set<() => void>()

function tabsOf(operationId: string): WorkPaneTabs {
  let tabs = kept.get(operationId)
  if (!tabs) {
    tabs = load(operationId)
    kept.set(operationId, tabs)
  }
  return tabs
}

/** Sets an operation's tabs: kept, and every page showing them told. */
export function setSessionTabs(operationId: string, next: WorkPaneTabs) {
  save(operationId, next)
  kept.set(operationId, next)
  for (const listener of listeners) listener()
}

/** Opens a session's tab (Interact) in its operation's work pane, in front. */
export function openSessionTab(operationId: string, sessionId: string) {
  setSessionTabs(operationId, openTab(tabsOf(operationId), sessionId))
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** The operation's session tabs, and a setter that keeps them. */
export function useSessionTabs(operationId: string): [WorkPaneTabs, (next: WorkPaneTabs) => void] {
  // Per operation: switching operations reads that one's.
  const tabs = useSyncExternalStore(subscribe, () => tabsOf(operationId))
  const set = useCallback((next: WorkPaneTabs) => setSessionTabs(operationId, next), [operationId])
  return [tabs, set]
}

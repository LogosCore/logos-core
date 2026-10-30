import { useCallback, useState } from "react"
import { NO_TABS, readTabs, type WorkPaneTabs } from "@/components/work-pane/work-pane-tabs"

// The session tabs of an operation's work pane, kept in this browser: an
// operator's desk, not the team's, and a reload or a later visit finds them
// as left. Storage can be missing or refuse (a private window): the tabs
// then last as long as the page.

const key = (operationId: string) => `logos.sessionTabs.${operationId}`

function load(operationId: string): WorkPaneTabs {
  try {
    const raw = localStorage.getItem(key(operationId))
    return raw ? readTabs(JSON.parse(raw)) : NO_TABS
  } catch {
    return NO_TABS
  }
}

function save(operationId: string, tabs: WorkPaneTabs) {
  try {
    if (tabs.ids.length === 0) localStorage.removeItem(key(operationId))
    else localStorage.setItem(key(operationId), JSON.stringify(tabs))
  } catch {
    // Kept for the page only.
  }
}

/** The operation's session tabs, and a setter that keeps them. */
export function useSessionTabs(operationId: string): [WorkPaneTabs, (next: WorkPaneTabs) => void] {
  // Per operation: switching operations reads that one's.
  const [state, setState] = useState(() => ({ operationId, tabs: load(operationId) }))
  const tabs = state.operationId === operationId ? state.tabs : load(operationId)
  const set = useCallback(
    (next: WorkPaneTabs) => {
      save(operationId, next)
      setState({ operationId, tabs: next })
    },
    [operationId],
  )
  return [tabs, set]
}

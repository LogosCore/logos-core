import { create } from "zustand"
import { createJSONStorage, persist } from "zustand/middleware"
import { toggleHighlight } from "@/components/modules/management/log-highlights"

// The module log fields highlighted (components/modules/management/
// log-highlights.ts), by module kind ("channel:http"): modules of a kind log
// the same fields. Kept in this browser, so the Logs tab opens with them.

interface LogHighlightsState {
  byKind: Record<string, string[]>
  toggle: (kind: string, path: string) => void
}

export const useLogHighlightsStore = create<LogHighlightsState>()(
  persist(
    (set) => ({
      byKind: {},
      toggle: (kind, path) =>
        set((s) => {
          const next = toggleHighlight(s.byKind[kind] ?? [], path)
          const byKind = { ...s.byKind }
          if (next.length > 0) byKind[kind] = next
          else delete byKind[kind]
          return { byKind }
        }),
    }),
    {
      name: "module-log-highlights",
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({ byKind: s.byKind }),
    },
  ),
)

// One empty list for every kind without highlights: a selector's answer
// must be the same value while nothing changes.
const NONE: readonly string[] = []

/** A module kind's highlighted fields (highlightsKey), and the toggle. */
export function useLogHighlights(kind: string): { highlights: readonly string[]; toggle: (path: string) => void } {
  const highlights = useLogHighlightsStore((s) => s.byKind[kind] ?? NONE)
  const toggleIn = useLogHighlightsStore((s) => s.toggle)
  return { highlights, toggle: (path: string) => toggleIn(kind, path) }
}

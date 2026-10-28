import { create } from "zustand"
import { createJSONStorage, persist } from "zustand/middleware"

// Whether a record's editor opens on its form or on its JSON (the endpoint
// modal's Form and JSON tabs, and every such choice after it). One
// preference for all of them, kept in this browser: the tab picked last is
// the one that opens next time, after a reload too.
export type EditorMode = "form" | "json"

interface EditorModeState {
  mode: EditorMode
  setMode: (mode: EditorMode) => void
}

export const useEditorModeStore = create<EditorModeState>()(
  persist(
    (set) => ({
      mode: "form",
      setMode: (mode) => set({ mode }),
    }),
    {
      name: "editor-mode",
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({ mode: s.mode }),
      merge: (persisted, current) => {
        const mode = (persisted as Partial<EditorModeState> | undefined)?.mode
        return { ...current, mode: mode === "json" || mode === "form" ? mode : current.mode }
      },
    },
  ),
)

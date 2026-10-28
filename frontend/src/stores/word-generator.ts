import { create } from "zustand"
import { createJSONStorage, persist } from "zustand/middleware"
import type { GeneratorSettings } from "@/lib/word-generator"

// The value generator's settings by field format, kept in this browser: it
// opens as it was left, after a reload too. What is read back is made whole
// by settingsOf (lib/word-generator.ts), so an old or broken entry is safe.

interface WordGeneratorState {
  saved: Record<string, unknown>
  save: (format: string, settings: GeneratorSettings) => void
  forget: (format: string) => void
}

export const useWordGeneratorStore = create<WordGeneratorState>()(
  persist(
    (set) => ({
      saved: {},
      save: (format, settings) => set((s) => ({ saved: { ...s.saved, [format]: settings } })),
      forget: (format) =>
        set((s) => {
          const saved = { ...s.saved }
          delete saved[format]
          return { saved }
        }),
    }),
    {
      name: "word-generator",
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({ saved: s.saved }),
    },
  ),
)

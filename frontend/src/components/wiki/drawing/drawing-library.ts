// The operator's own shape library, kept in this browser.
//
// Excalidraw's "Add to library" works without a host, but it only lives in
// memory. It was empty again after every reload, which made it look broken.
// This persists it to localStorage through the adapter useHandleLibrary
// takes. The Shapes panel (drawing-shapes-panel.tsx) shows these as "My
// shapes", next to the built-ins, which ship with the app and are never
// stored.
//
// Personal and browser-local, like the grid preference. A library an
// operation edits together would need a backend model, so that is a separate
// decision.

import type { LibraryPersistenceAdapter } from "@excalidraw/excalidraw/data/library"

const STORAGE_KEY = "wiki_drawing_library"

export const drawingLibraryAdapter: LibraryPersistenceAdapter = {
  load() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (!raw) return null
      const parsed: unknown = JSON.parse(raw)
      const items = (parsed as { libraryItems?: unknown } | null)?.libraryItems
      return Array.isArray(items) && items.length > 0 ? { libraryItems: items } : null
    } catch {
      return null
    }
  },
  save({ libraryItems }) {
    // Allowed to throw: Excalidraw reports a failed save to the operator,
    // which is better than a library that silently stops being kept. A full
    // quota is the realistic failure.
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ libraryItems }))
  },
}

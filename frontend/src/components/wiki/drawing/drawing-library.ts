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
import type { LibraryItems } from "@excalidraw/excalidraw/types"

const STORAGE_KEY = "wiki_drawing_library"

/** The items in a stored library, or null for none or a damaged entry. */
function parseStored(raw: string | null): LibraryItems | null {
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    const items = (parsed as { libraryItems?: unknown } | null)?.libraryItems
    return Array.isArray(items) && items.length > 0 ? (items as LibraryItems) : null
  } catch {
    return null
  }
}

export const drawingLibraryAdapter: LibraryPersistenceAdapter = {
  load() {
    try {
      const items = parseStored(localStorage.getItem(STORAGE_KEY))
      return items ? { libraryItems: items } : null
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

/**
 * Follow the library as other tabs change it.
 *
 * Each canvas read the library once, at load, and every save wrote its whole
 * list back. With two drawings open, a shape added in one tab was erased by
 * the next save in the other, which had never seen it. A `storage` event
 * fires in every other tab of this origin when one writes the key, so each
 * canvas now takes the new list as it lands and its next save starts from it.
 * Two tabs saving in the same instant can still race; that window is the
 * width of one write, not the whole session.
 *
 * Returns the unsubscribe function. `target` is for tests.
 */
export function followLibraryInOtherTabs(
  onChange: (items: LibraryItems) => void,
  target: EventTarget = window,
): () => void {
  const onStorage = (event: Event) => {
    const { key, newValue } = event as StorageEvent
    // A null key is localStorage.clear(), which empties this key too.
    if (key !== STORAGE_KEY && key !== null) return
    onChange(parseStored(newValue) ?? [])
  }
  target.addEventListener("storage", onStorage)
  return () => target.removeEventListener("storage", onStorage)
}

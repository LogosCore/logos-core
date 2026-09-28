// Whether the drawing canvas shows its grid, remembered per browser.
//
// On by default: most of what gets drawn here is boxes and arrows, which are
// easier to line up against a grid. Once the operator toggles it (the canvas
// menu, or Ctrl/Cmd+'), that choice sticks across drawings and reloads.
//
// A viewer preference, not part of the drawing: Excalidraw keeps it in
// appState.gridModeEnabled, which the scene sync never shares
// (drawing-scene.ts — SharedAppState), so one person hiding the grid does not
// hide it for anyone else.

const STORAGE_KEY = "wiki_drawing_grid"

export function readGridPreference(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== "off"
  } catch {
    return true
  }
}

export function writeGridPreference(enabled: boolean) {
  try {
    localStorage.setItem(STORAGE_KEY, enabled ? "on" : "off")
  } catch {
    // Storage unavailable: the grid simply starts on next time.
  }
}

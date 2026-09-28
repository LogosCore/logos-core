// The Excalidraw canvas itself.
//
// This module is the only place that imports the library, and it is loaded
// through React.lazy from wiki-drawing-pane.tsx. That is not a
// micro-optimisation: @excalidraw/excalidraw unpacks to ~47 MB across roughly
// thirty transitive dependencies, and importing it anywhere in the eager graph
// would put all of it in front of every operator who opens a prose page.
//
// Its stylesheet is imported here for the same reason — Vite emits it as the
// chunk's own CSS, fetched with the chunk rather than with the app.

import { useCallback, useEffect, useRef, useState } from "react"
import { Excalidraw } from "@excalidraw/excalidraw"
import type { AppState, BinaryFiles } from "@excalidraw/excalidraw/types"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import type { Doc as YDoc } from "yjs"
import type { HocuspocusProvider } from "@hocuspocus/provider"
import { useTheme } from "next-themes"

import { useDrawingSync } from "./use-drawing-sync"
import { readGridPreference, writeGridPreference } from "./drawing-grid-preference"
import "@excalidraw/excalidraw/index.css"
import "./wiki-drawing.css"

interface WikiDrawingCanvasProps {
  documentId: string
  ydoc: YDoc
  provider: HocuspocusProvider | null
  isEditor: boolean
  user: { userId: string; username: string } | null
  /** Fires once the canvas is mounted and showing the synced scene. The print
   * route uses it as its "safe to print" signal. */
  onReady?: () => void
}

export default function WikiDrawingCanvas({
  documentId,
  ydoc,
  provider,
  isEditor,
  user,
  onReady,
}: WikiDrawingCanvasProps) {
  const { resolvedTheme } = useTheme()
  const {
    onExcalidrawAPI,
    onChange,
    onPointerUpdate,
    initialElements,
    initialBackgroundColor,
    initialFiles,
  } = useDrawingSync({ documentId, ydoc, provider, isEditor, user })

  // Grid on by default, then whatever this browser last chose. Seeded through
  // initialData rather than the gridModeEnabled prop: the prop makes the grid
  // controlled, and Excalidraw then hides its own toggle.
  const [initialGrid] = useState(readGridPreference)
  const gridRef = useRef(initialGrid)
  const handleChange = useCallback(
    (elements: readonly ExcalidrawElement[], appState: AppState, files: BinaryFiles) => {
      onChange(elements, appState, files)
      // onChange fires on every pointer move; write only on an actual toggle.
      if (appState.gridModeEnabled !== gridRef.current) {
        gridRef.current = appState.gridModeEnabled
        writeGridPreference(appState.gridModeEnabled)
      }
    },
    [onChange],
  )

  // Fire once. onReady is a lifecycle signal, not a subscription — a print
  // route that heard it twice would call window.print() twice.
  const readyFiredRef = useRef(false)
  useEffect(() => {
    if (readyFiredRef.current) return
    readyFiredRef.current = true
    onReady?.()
  }, [onReady])

  return (
    <div className="wiki-drawing-canvas flex min-h-0 flex-1">
      <Excalidraw
        excalidrawAPI={onExcalidrawAPI}
        initialData={{
          elements: initialElements,
          // Image bytes are not in the CRDT; these point at the wiki image
          // endpoint, so the browser fetches and caches them like any image.
          files: initialFiles,
          appState: {
            viewBackgroundColor: initialBackgroundColor,
            gridModeEnabled: initialGrid,
            // Excalidraw persists its own appState to localStorage by default
            // via the host app; we own persistence, so start from the scene.
            collaborators: new Map(),
          },
          scrollToContent: true,
        }}
        onChange={handleChange}
        onPointerUpdate={onPointerUpdate}
        // Tells Excalidraw to render other people's cursors at all. Without it
        // the collaborators map is accepted and silently ignored.
        isCollaborating
        // Read-only for viewers. The server already rejects their updates
        // (the collab ticket flags the connection read-only), so this is about
        // not offering tools that would do nothing.
        viewModeEnabled={!isEditor}
        theme={resolvedTheme === "dark" ? "dark" : "light"}
        UIOptions={{
          canvasActions: {
            // Excalidraw's own load/save act on local files and its own
            // storage, which would sit beside this page's real persistence and
            // confuse where a drawing actually lives.
            loadScene: false,
            saveToActiveFile: false,
            export: false,
            saveAsImage: true,
          },
        }}
      />
    </div>
  )
}

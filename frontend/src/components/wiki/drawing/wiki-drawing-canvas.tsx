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
import { Excalidraw, useHandleLibrary } from "@excalidraw/excalidraw"
import type {
  AppState,
  BinaryFiles,
  ExcalidrawImperativeAPI,
  LibraryItems,
  NormalizedZoomValue,
} from "@excalidraw/excalidraw/types"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import type { Doc as YDoc } from "yjs"
import type { HocuspocusProvider } from "@hocuspocus/provider"
import { useTheme } from "next-themes"
import { Maximize } from "lucide-react"

import { useDrawingSync } from "./use-drawing-sync"
import { readGridPreference, writeGridPreference } from "./drawing-grid-preference"
import { loadDrawingView, saveDrawingView, type DrawingView } from "./drawing-view-memory"
import { drawingLibraryAdapter, followLibraryInOtherTabs } from "./drawing-library"
import { embedWikiImages } from "./drawing-images"
import { ShapesSidebar, ShapesTrigger } from "./drawing-shapes-panel"
import { useRecordCards } from "./use-record-cards"
import { RecordOverlay } from "./drawing-record-overlay"
import "@excalidraw/excalidraw/index.css"
import "./wiki-drawing.css"

interface WikiDrawingCanvasProps {
  documentId: string
  /** The page's operation: record cards are picked from it. */
  operationId: string
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
  operationId,
  ydoc,
  provider,
  isEditor,
  user,
  onReady,
}: WikiDrawingCanvasProps) {
  const { resolvedTheme } = useTheme()
  const {
    api,
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
  const trackExportDialog = useEmbeddedImagesForExport(api)
  const containerRef = useRef<HTMLDivElement>(null)
  const records = useRecordCards({ api, isEditor, operationId, documentId, containerRef })
  const recordsChange = records.onChange
  const handleChange = useCallback(
    (elements: readonly ExcalidrawElement[], appState: AppState, files: BinaryFiles) => {
      onChange(elements, appState, files)
      recordsChange(elements, appState)
      trackExportDialog(appState)
      // onChange fires on every pointer move; write only on an actual toggle.
      if (appState.gridModeEnabled !== gridRef.current) {
        gridRef.current = appState.gridModeEnabled
        writeGridPreference(appState.gridModeEnabled)
      }
    },
    [onChange, recordsChange, trackExportDialog],
  )

  // The operator's own shapes survive a reload (drawing-library.ts), and the
  // Shapes panel lists them next to the built-ins.
  useHandleLibrary({ excalidrawAPI: api, adapter: drawingLibraryAdapter })
  // And kept in step with other tabs, so one tab's save cannot erase what
  // another just added. See followLibraryInOtherTabs.
  useEffect(() => {
    if (!api) return
    return followLibraryInOtherTabs((libraryItems) => {
      void api.updateLibrary({ libraryItems, merge: false })
    })
  }, [api])
  const [personalShapes, setPersonalShapes] = useState<LibraryItems>([])
  const dark = resolvedTheme === "dark"

  // Reopen where this browser last left the drawing, rather than fitted to
  // its content every time. Fitting is still the first visit's view.
  const [initialView] = useState(() => loadDrawingView(documentId))

  // Without a remembered view, open fitted to the whole drawing, zoomed out
  // as far as it takes. Excalidraw's own scrollToContent on load only centres
  // the content at 100%, which shows a corner of any drawing larger than the
  // screen. Waits for the canvas to have a size and the scene to be loaded:
  // fitting before either measures nothing.
  const fittedRef = useRef(false)
  useEffect(() => {
    if (!api || initialView || fittedRef.current) return
    let frame = 0
    let tries = 0
    const attempt = () => {
      const ready = api.getAppState().width > 0 && api.getSceneElements().length > 0
      if (ready || tries++ > 30) {
        fittedRef.current = true
        fitToContent(api, false)
        return
      }
      frame = requestAnimationFrame(attempt)
    }
    frame = requestAnimationFrame(attempt)
    return () => cancelAnimationFrame(frame)
  }, [api, initialView])
  const onScrollChange = useDrawingViewMemory(documentId)

  // Fire once. onReady is a lifecycle signal, not a subscription — a print
  // route that heard it twice would call window.print() twice.
  const readyFiredRef = useRef(false)
  useEffect(() => {
    if (readyFiredRef.current) return
    readyFiredRef.current = true
    onReady?.()
  }, [onReady])

  return (
    <div ref={containerRef} className="wiki-drawing-canvas relative flex min-h-0 flex-1">
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
            ...(initialView && {
              scrollX: initialView.scrollX,
              scrollY: initialView.scrollY,
              zoom: { value: initialView.zoom as NormalizedZoomValue },
            }),
            // Excalidraw persists its own appState to localStorage by default
            // via the host app; we own persistence, so start from the scene.
            collaborators: new Map(),
          },
          scrollToContent: !initialView,
        }}
        onChange={handleChange}
        onScrollChange={onScrollChange}
        onPointerUpdate={onPointerUpdate}
        // Record cards open their record here rather than following the
        // link (use-record-cards.ts); any other link behaves as Excalidraw's.
        onLinkOpen={records.onLinkOpen}
        // A copied chip, or a link to a record, pastes as its card.
        onPaste={records.onPaste}
        // Tells Excalidraw to render other people's cursors at all. Without it
        // the collaborators map is accepted and silently ignored.
        isCollaborating
        // Read-only for viewers. The server already rejects their updates
        // (the collab ticket flags the connection read-only), so this is about
        // not offering tools that would do nothing.
        viewModeEnabled={!isEditor}
        theme={dark ? "dark" : "light"}
        onLibraryChange={setPersonalShapes}
        // The Shapes panel's button, beside Excalidraw's own Library one.
        // Editors only: a viewer cannot place anything.
        renderTopRightUI={() => (
          <>
            {/* For viewers too: anyone can lose their place in a big map. */}
            <FitButton api={api} />
            {isEditor && <ShapesTrigger />}
          </>
        )}
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
      >
        {isEditor && (
          <ShapesSidebar api={api} personal={personalShapes} dark={dark} onAddRecord={records.addRecord} />
        )}
      </Excalidraw>
      <RecordOverlay items={records.overlay} onOpen={records.openCard} />
    </div>
  )
}

/**
 * Embed the drawing's pictures while Excalidraw's image export dialog is
 * open, so a downloaded SVG carries them instead of links that only work
 * inside the app. See embedWikiImages.
 *
 * Returns a function for onChange, which is where the dialog's opening and
 * closing can be seen. The bytes are fetched once the dialog opens, from the
 * browser cache in the usual case, so an SVG saved the instant the dialog
 * appears can still go out with links.
 */
function useEmbeddedImagesForExport(api: ExcalidrawImperativeAPI | null) {
  const openRef = useRef(false)
  const restoreRef = useRef<Promise<() => void> | null>(null)

  const close = useCallback(() => {
    const pending = restoreRef.current
    restoreRef.current = null
    void pending?.then((restore) => restore())
  }, [])

  // Not left embedded if the canvas goes away with the dialog open.
  useEffect(() => close, [close])

  return useCallback(
    (appState: AppState) => {
      const open = appState.openDialog?.name === "imageExport"
      if (open === openRef.current) return
      openRef.current = open
      if (open && api) {
        restoreRef.current = embedWikiImages(api.getFiles())
      } else {
        close()
      }
    },
    [api, close],
  )
}

// How long the view has to stay put before it is written. A pan fires a scroll
// change per frame; the write is the one at the end.
const VIEW_WRITE_DELAY_MS = 250

/** Record the view as the operator pans and zooms. A write still waiting is
 * sent when the page is left or the drawing closes. */
function useDrawingViewMemory(documentId: string) {
  const pendingRef = useRef<DrawingView | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const flush = useCallback(() => {
    clearTimeout(timerRef.current)
    timerRef.current = undefined
    if (pendingRef.current) saveDrawingView(pendingRef.current)
    pendingRef.current = null
  }, [])

  useEffect(() => {
    window.addEventListener("pagehide", flush)
    return () => {
      window.removeEventListener("pagehide", flush)
      flush()
    }
  }, [flush])

  return useCallback(
    (scrollX: number, scrollY: number, zoom: { value: number }) => {
      pendingRef.current = { documentId, scrollX, scrollY, zoom: zoom.value }
      clearTimeout(timerRef.current)
      timerRef.current = setTimeout(flush, VIEW_WRITE_DELAY_MS)
    },
    [documentId, flush],
  )
}

/**
 * Zoom and scroll so the whole drawing fills the canvas. Never zooms in past
 * 100%: a drawing of two shapes fitted to a large screen would otherwise come
 * out enormous. Excalidraw has the same as Shift+1; this is for those who do
 * not know that.
 */
function fitToContent(api: ExcalidrawImperativeAPI, animate: boolean) {
  const elements = api.getSceneElements()
  if (elements.length === 0) return
  api.scrollToContent(elements, {
    fitToViewport: true,
    viewportZoomFactor: 0.92,
    maxZoom: 1,
    animate,
    duration: 350,
  })
}

/** Styled with Excalidraw's own class for its top-right buttons, so it sits
 * beside Library and Shapes as one of them. */
function FitButton({ api }: { api: ExcalidrawImperativeAPI | null }) {
  return (
    <button
      type="button"
      className="sidebar-trigger"
      title="Fit the whole drawing on screen (Shift+1)"
      aria-label="Fit the whole drawing on screen"
      disabled={!api}
      onClick={() => api && fitToContent(api, true)}
    >
      <div>
        <Maximize className="size-4" />
      </div>
      Fit
    </button>
  )
}

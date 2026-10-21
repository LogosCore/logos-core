import { useEffect, useMemo, useRef, useState } from "react"
import { EditorContent, useEditor } from "@tiptap/react"
import type { JSONContent } from "@tiptap/core"
import { viewerExtensions } from "./viewer-extensions"
import { loadPage, type ExcalidrawFileData, type ExportPage, type PageBody } from "./viewer-data"
import { loadDrawingRenderer } from "./viewer-drawing"
import { PanZoom } from "./viewer-pan-zoom"
import { usePageHighlight } from "./viewer-page-highlight"

type State =
  | { status: "loading" }
  | { status: "ready"; body: PageBody }
  | { status: "error"; message: string }

/** `highlight` holds the active search's words; their matches are painted
 * on the page and the first is scrolled into view. */
export function ViewerPageBody({ page, dark, highlight }: { page: ExportPage; dark: boolean; highlight: string[] }) {
  if (page.missing) {
    return <p className="text-sm text-muted-foreground">This page's content could not be exported.</p>
  }
  // Keyed, so every page starts from "loading" instead of showing the last
  // one's body while its own script arrives.
  return <LoadedPage key={page.id} id={page.id} dark={dark} highlight={highlight} />
}

function LoadedPage({ id, dark, highlight }: { id: string; dark: boolean; highlight: string[] }) {
  const [state, setState] = useState<State>({ status: "loading" })

  useEffect(() => {
    let cancelled = false
    loadPage(id).then(
      (body) => !cancelled && setState({ status: "ready", body }),
      (err: Error) => !cancelled && setState({ status: "error", message: err.message }),
    )
    return () => {
      cancelled = true
    }
  }, [id])

  if (state.status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading…</p>
  }
  if (state.status === "error") {
    return <p className="text-sm text-muted-foreground">{state.message}</p>
  }
  if (state.body.kind === "drawing") {
    return <DrawingView scene={state.body.scene} dark={dark} />
  }
  return <DocumentView id={id} doc={state.body.doc} highlight={highlight} />
}

function DocumentView({ id, doc, highlight }: { id: string; doc: JSONContent; highlight: string[] }) {
  const rootRef = useRef<HTMLDivElement | null>(null)
  usePageHighlight(rootRef, highlight, id)
  const extensions = useMemo(() => viewerExtensions(), [])
  const editor = useEditor({
    extensions,
    content: doc,
    editable: false,
    immediatelyRender: true,
  })
  // Links between pages in the export are `#/page/<id>`. The editor's link
  // mark opens every link in a new tab, which for these would mean a second
  // copy of the viewer; they navigate in place instead.
  const onClickCapture = (e: React.MouseEvent) => {
    const a = (e.target as HTMLElement).closest("a")
    const href = a?.getAttribute("href")
    if (!href?.startsWith("#/")) return
    e.preventDefault()
    window.location.hash = href
  }
  return (
    <div ref={rootRef} onClickCapture={onClickCapture}>
      <EditorContent
        editor={editor}
        className="prose prose-sm dark:prose-invert max-w-none focus:outline-none"
      />
    </div>
  )
}

function DrawingView({ scene, dark }: { scene: ExcalidrawFileData; dark: boolean }) {
  const empty = useMemo(
    () => !scene.elements.some((el) => !(el as { isDeleted?: boolean }).isDeleted),
    [scene],
  )
  if (empty) return <p className="text-sm text-muted-foreground">This drawing is empty.</p>
  // Keyed by theme: a fresh render per theme, with no stale error carried over.
  return <DrawingSvg key={dark ? "dark" : "light"} scene={scene} dark={dark} />
}

function DrawingSvg({ scene, dark }: { scene: ExcalidrawFileData; dark: boolean }) {
  const [rendered, setRendered] = useState<{ svg: SVGSVGElement; width: number; height: number } | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    loadDrawingRenderer()
      .then((renderer) => renderer.renderSvg(scene, { dark }))
      .then((svg) => {
        if (cancelled) return
        // Rendered at its natural size; the pan-zoom viewport does the
        // scaling, so a wide diagram can be read at 100% and beyond.
        const width = Number(svg.getAttribute("width")) || svg.viewBox.baseVal.width || 800
        const height = Number(svg.getAttribute("height")) || svg.viewBox.baseVal.height || 600
        svg.setAttribute("width", String(width))
        svg.setAttribute("height", String(height))
        setRendered({ svg, width, height })
      })
      .catch((err: Error) => !cancelled && setError(`The drawing could not be rendered: ${err.message}`))
    return () => {
      cancelled = true
    }
  }, [scene, dark])

  if (error) return <p className="text-sm text-muted-foreground">{error}</p>
  if (!rendered) return <p className="text-sm text-muted-foreground">Rendering…</p>
  return (
    <PanZoom width={rendered.width} height={rendered.height}>
      <SvgHost svg={rendered.svg} />
    </PanZoom>
  )
}

/** Mounts an SVG element React did not create. */
function SvgHost({ svg }: { svg: SVGSVGElement }) {
  const ref = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    ref.current?.replaceChildren(svg)
  }, [svg])
  return <div ref={ref} className="wiki-viewer-drawing" />
}

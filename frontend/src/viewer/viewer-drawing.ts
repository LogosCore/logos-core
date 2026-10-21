// Loads assets/drawing.js, the separate bundle that carries Excalidraw's SVG
// export, the first time a drawing is opened. Excalidraw is most of a
// megabyte; a bundle of prose pages never pays for it.

import type { ExcalidrawFileData } from "./viewer-data"

export interface DrawingRenderer {
  renderSvg(scene: ExcalidrawFileData, opts: { dark: boolean }): Promise<SVGSVGElement>
}

declare global {
  interface Window {
    LogosDrawing?: DrawingRenderer
  }
}

let loading: Promise<DrawingRenderer> | null = null

export function loadDrawingRenderer(): Promise<DrawingRenderer> {
  if (window.LogosDrawing) return Promise.resolve(window.LogosDrawing)
  loading ??= new Promise<DrawingRenderer>((resolve, reject) => {
    const script = document.createElement("script")
    script.src = "assets/drawing.js"
    script.async = true
    script.onload = () =>
      window.LogosDrawing
        ? resolve(window.LogosDrawing)
        : reject(new Error("assets/drawing.js did not load the renderer"))
    script.onerror = () => {
      loading = null
      reject(new Error("assets/drawing.js is missing from the export"))
    }
    document.head.appendChild(script)
  })
  return loading
}

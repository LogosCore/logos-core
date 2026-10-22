// Entry point of assets/drawing.js: Excalidraw's SVG export, for the HTML
// bundle's viewer (see viewer-drawing.ts). Built as its own classic script,
// so pages that hold no drawing never load it.
//
// Fonts. exportToSvg embeds the fonts a scene uses by fetching them, and a
// page opened from disk cannot fetch: file:// requests are refused, and
// Excalidraw's fallback is its CDN, which is the internet. So the fonts ride
// inside this bundle as data URLs and fetch is answered from them for any
// URL that names one. Xiaolai (CJK, 13 MB) is left out; CJK text in a
// drawing falls back to a system font.

import { exportToSvg, getCommonBounds } from "@excalidraw/excalidraw"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import type { AppState, BinaryFiles } from "@excalidraw/excalidraw/types"
import type { DrawingRenderer } from "./viewer-drawing"
import type { ExcalidrawFileData } from "./viewer-data"

const FONTS = import.meta.glob(
  "/node_modules/@excalidraw/excalidraw/dist/prod/fonts/{Assistant,Cascadia,ComicShanns,Excalifont,Liberation,Lilita,Nunito,Virgil}/*.woff2",
  { query: "?inline", import: "default", eager: true },
) as Record<string, string>

/** "Excalifont/Excalifont-Regular-<hash>.woff2" → data URL. */
const fontByName = new Map<string, string>()
for (const [path, dataURL] of Object.entries(FONTS)) {
  const m = /fonts\/([^/]+\/[^/]+\.woff2)$/.exec(path)
  if (m) fontByName.set(m[1], dataURL)
}

const realFetch = window.fetch.bind(window)
window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
  const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
  const m = /fonts\/([^/?#]+\/[^/?#]+\.woff2)(?:[?#]|$)/.exec(url)
  const dataURL = m && fontByName.get(m[1])
  return dataURL ? realFetch(dataURL) : realFetch(input, init)
}

/** A record card's frame links to the record in the app, which from a page
 * on disk is a dead path; the viewer opens the card itself (viewer-page.tsx). */
function withoutRecordLinks(el: ExcalidrawElement): ExcalidrawElement {
  const ref = (el.customData as { logos?: { ref?: unknown } } | undefined)?.logos?.ref
  return ref && el.link ? { ...el, link: null } : el
}

const EXPORT_PADDING = 16

const renderer: DrawingRenderer = {
  async renderSvg(scene: ExcalidrawFileData, { dark }) {
    const elements = (scene.elements as ExcalidrawElement[]).filter((el) => !el.isDeleted).map(withoutRecordLinks)
    const [minX, minY] = getCommonBounds(elements)
    const svg = await exportToSvg({
      elements,
      appState: {
        ...(scene.appState as Partial<AppState>),
        exportBackground: false,
        exportWithDarkMode: dark,
      },
      files: (scene.files ?? {}) as BinaryFiles,
      exportPadding: EXPORT_PADDING,
    })
    return { svg, origin: { x: minX - EXPORT_PADDING, y: minY - EXPORT_PADDING } }
  },
}

window.LogosDrawing = renderer

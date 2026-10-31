import { useRef, useState, type ReactElement } from "react"
import { useTheme } from "next-themes"
import Lightbox from "yet-another-react-lightbox"
import Zoom from "yet-another-react-lightbox/plugins/zoom"
import Fullscreen from "yet-another-react-lightbox/plugins/fullscreen"
import "yet-another-react-lightbox/styles.css"

import type { FilePreviewKind } from "./kind"
import { FilePreviewPanel } from "./preview-panel"
import { useRenderedPreview } from "./source"
import { useDecodedImage } from "./use-decoded-image"
import "./file-preview.css"

// A file's preview filling the space it is given, for a page that shows one
// file (the session console's file view), where the wiki's attachment card
// shows it in a panel under the card. The same renderers, by the same
// rules: a picture drawn by the browser (or decoded first) and opened full
// size on a click; a PDF, video or audio from the inline URL; anything
// else converted into a locked-down frame (source.ts).

export function FilePreviewPane({
  url,
  kind,
  filename,
  size,
}: {
  /** The file's bytes; with ?preview=1, served inline (kind.ts). */
  url: string
  kind: FilePreviewKind
  filename: string
  size: number
}): ReactElement {
  const inlineUrl = `${url}${url.includes("?") ? "&" : "?"}preview=1`
  const { resolvedTheme } = useTheme()
  const rendered = useRenderedPreview(url, kind.renderedKind, kind.renderedKind !== null, resolvedTheme === "dark" ? "dark" : "light", filename)
  const containerRef = useRef<HTMLDivElement>(null)
  const imageRef = useRef<HTMLDivElement>(null)
  const decoded = useDecodedImage(
    kind.imageFormat === "native" ? null : kind.imageFormat,
    url,
    size,
    imageRef,
    true,
  )
  const [lightbox, setLightbox] = useState(false)
  const [imageFailed, setImageFailed] = useState(false)

  if (kind.imageFormat !== null) {
    const src = kind.imageFormat === "native" ? inlineUrl : decoded.src
    return (
      <div ref={imageRef} className="flex min-h-0 flex-1 items-center justify-center overflow-hidden p-4">
        {imageFailed || decoded.failed ? (
          <p className="text-sm text-muted-foreground">This browser can&rsquo;t show this picture.</p>
        ) : src === null ? (
          <p className="text-sm text-muted-foreground">Decoding the picture…</p>
        ) : (
          // The button fills the pane and the picture is fitted in it: a
          // button the size of its picture gives max-h-full nothing to be a
          // share of, and a tall picture ran under the pane's edge.
          <button type="button" className="relative size-full cursor-zoom-in" title="Open full size" onClick={() => setLightbox(true)}>
            <img
              src={src}
              alt={filename}
              className="absolute inset-0 m-auto max-h-full max-w-full object-contain [background:repeating-conic-gradient(var(--color-muted)_0_25%,transparent_0_50%)_0_0/16px_16px]"
              draggable={false}
              onError={() => setImageFailed(true)}
            />
          </button>
        )}
        {lightbox && src !== null && (
          <Lightbox
            open
            close={() => setLightbox(false)}
            slides={[{ src, alt: filename }]}
            plugins={[Zoom, Fullscreen]}
            carousel={{ finite: true }}
            controller={{ closeOnBackdropClick: true }}
            render={{ buttonPrev: () => null, buttonNext: () => null }}
            zoom={{ maxZoomPixelRatio: 8, scrollToZoom: true }}
          />
        )}
      </div>
    )
  }

  return (
    <FilePreviewPanel
      containerRef={containerRef}
      isPdf={kind.isPdf}
      mediaKind={kind.mediaKind}
      previewUrl={inlineUrl}
      filename={filename}
      rendered={rendered}
      startActive
      fill
    />
  )
}

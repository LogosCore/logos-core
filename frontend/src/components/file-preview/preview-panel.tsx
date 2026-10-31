import { useEffect, useRef, useState, type CSSProperties, type ReactElement, type RefObject } from "react"

import { cn } from "@/lib/utils"
import { estimateTextHeight } from "./fit"
import type { MediaPreviewKind } from "./media"
import { PreviewResizeHandle } from "./resize"
import type { RenderedPreviewState } from "./source"
import "./file-preview.css"

export interface FilePreviewPanelProps {
  /** Container ref — the element handed to the native Fullscreen request and
   *  the target the resize drag mutates in place. */
  containerRef: RefObject<HTMLDivElement | null>
  isPdf: boolean
  /** Video or audio rendered through a native player; null otherwise. */
  mediaKind: MediaPreviewKind | null
  /** Inline-disposition URL for the PDF iframe and the media player (unused for HTML). */
  previewUrl: string
  filename: string
  rendered: RenderedPreviewState
  /** User-dragged height in px, or null for the CSS default. */
  height?: number | null
  /** Commits a new dragged height once the drag ends. */
  onHeightChange?: (height: number) => void
  /** The frame takes the pointer from the start, without a first click. */
  startActive: boolean
  /** Fills its parent, which sizes it: no frame around it, no drag handle,
   *  the frame always active. The panel in a page of its own, as opposed to
   *  one among a document's. */
  fill?: boolean
}

// The preview panel. PDFs render via an inline iframe src; every other format
// renders a converted document into a fully locked sandbox (no allow-scripts,
// no allow-same-origin) so embedded scripts are inert, carrying an injected
// CSP that also denies the document any network egress, while inline CSS and
// data-URI assets still display. Inline, a drag handle lets the reader grow or
// shrink the frame; the chosen height is held by the caller.
export function FilePreviewPanel({
  containerRef,
  isPdf,
  mediaKind,
  previewUrl,
  filename,
  rendered,
  height = null,
  onHeightChange,
  startActive,
  fill = false,
}: FilePreviewPanelProps): ReactElement {
  // Shared by whichever of the two iframes renders — the resize handle measures
  // this to seed the drag. Absent while HTML is still loading or errored, which
  // is exactly when we also hide the handle.
  const frameRef = useRef<HTMLIFrameElement>(null)
  // Media sizes itself to its own aspect ratio, so no drag handle.
  const hasFrame = mediaKind === null && (isPdf || rendered.content !== null)
  // An inactive frame has pointer-events disabled, so the wheel scrolls the
  // page past it; clicking the preview activates it. A panel the reader just
  // opened starts active: they opened it to read it. One restored open with
  // the page starts inactive, or scrolling down the page would stop dead
  // inside the first preview the pointer crossed.
  const [frameActive, setFrameActive] = useState(startActive || fill)
  // Set when the browser cannot decode the media; see the players below.
  const [mediaFailed, setMediaFailed] = useState(false)
  // A listing shorter than the frame caps it at its own height (see fit.ts).
  // Wrapped text depends on the width, so the panel's width is followed as
  // the page resizes.
  const [panelWidth, setPanelWidth] = useState(0)
  const layout = fill ? null : rendered.textLayout
  useEffect(() => {
    const panel = containerRef.current
    if (!panel || layout === null) return
    const observer = new ResizeObserver(() => setPanelWidth(panel.clientWidth))
    observer.observe(panel)
    return () => observer.disconnect()
  }, [containerRef, layout])
  const frameCap = layout !== null && panelWidth > 0 ? estimateTextHeight(layout, panelWidth) : null
  const frameStyle = {
    ...(frameActive ? {} : { pointerEvents: "none" as const }),
    ...(frameCap === null ? {} : { "--file-preview-max": `${frameCap}px` }),
  } as CSSProperties

  return (
    <div className={cn("file-preview", fill && "file-preview--fill")} contentEditable={false} ref={containerRef}>
      {mediaKind !== null && mediaFailed ? (
        <p className="file-preview-status file-preview-status--error">
          This browser can&rsquo;t play this {mediaKind === "video" ? "video" : "recording"}: it was
          encoded in a format the browser does not decode. Download it to watch it in a desktop
          player.
        </p>
      ) : mediaKind === "video" ? (
        // preload="metadata": duration and first frame only, until played.
        <video
          className="file-preview-media"
          controls
          preload="metadata"
          src={previewUrl}
          title={filename}
          onError={() => setMediaFailed(true)}
          onLoadedMetadata={(e) => {
            // A video track the browser cannot decode next to an audio track it
            // can raises no error: the file "plays", sound over a black frame.
            // No picture size is how that shows.
            if (e.currentTarget.videoWidth === 0) setMediaFailed(true)
          }}
        />
      ) : mediaKind === "audio" ? (
        <audio
          className="file-preview-media file-preview-media--audio"
          controls
          preload="metadata"
          src={previewUrl}
          title={filename}
          onError={() => setMediaFailed(true)}
        />
      ) : isPdf ? (
        <div className="file-preview-frame-wrap">
          <iframe
            ref={frameRef}
            className="file-preview-frame"
            src={previewUrl}
            title={`Preview of ${filename}`}
            style={frameActive ? undefined : { pointerEvents: "none" }}
          />
          {!frameActive && <div className="file-preview-frame-overlay" onClick={() => setFrameActive(true)} />}
        </div>
      ) : rendered.error !== null ? (
        <p className="file-preview-status file-preview-status--error">{rendered.error}</p>
      ) : rendered.content === null ? (
        <p className="file-preview-status">Loading preview…</p>
      ) : (
        <div className="file-preview-frame-wrap">
          <iframe
            ref={frameRef}
            className="file-preview-frame"
            sandbox=""
            srcDoc={rendered.content}
            title={`Preview of ${filename}`}
            style={frameStyle}
          />
          {!frameActive && <div className="file-preview-frame-overlay" onClick={() => setFrameActive(true)} />}
        </div>
      )}
      {hasFrame && !fill && onHeightChange ? (
        <PreviewResizeHandle containerRef={containerRef} frameRef={frameRef} height={height} onCommit={onHeightChange} />
      ) : null}
    </div>
  )
}

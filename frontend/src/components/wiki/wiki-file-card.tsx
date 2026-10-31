import {
  NodeViewWrapper,
  type ReactNodeViewProps,
} from "@tiptap/react"
import {
  ChevronDownIcon,
  DownloadIcon,
  ExternalLinkIcon,
  EyeIcon,
  Maximize2Icon,
  Trash2Icon,
} from "lucide-react"
import { useTheme } from "next-themes"
import {
  useEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
} from "react"

import Lightbox from "yet-another-react-lightbox"
import Zoom from "yet-another-react-lightbox/plugins/zoom"
import Fullscreen from "yet-another-react-lightbox/plugins/fullscreen"
import Counter from "yet-another-react-lightbox/plugins/counter"
import "yet-another-react-lightbox/styles.css"
import "yet-another-react-lightbox/plugins/counter.css"

import { usePrintMode } from "@/hooks/use-print-mode"
import {
  loadFilePreviewState,
  saveFilePreviewState,
  removeFilePreviewState,
} from "@/lib/wiki-file-preview-state"
import { useFocusRestoreWithoutScroll } from "@/hooks/use-focus-restore-without-scroll"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import {
  canonicalContentType,
  filePreviewKind,
  panelPreviews,
  servedInline,
} from "@/components/file-preview/kind"
import { FilePreviewPanel } from "@/components/file-preview/preview-panel"
import { useRenderedPreview } from "@/components/file-preview/source"
import { useDecodedImage } from "@/components/file-preview/use-decoded-image"
// Through the alias path, not "./": the viewer build replaces this module.
import { useServerContentType } from "@/components/wiki/wiki-file-type-probe"

interface FileNodeAttrs {
  fileId: string | null
  url: string | null
  filename: string
  size: number
  contentType: string
}

/** Lucide glyph size for the hover-action buttons. */
const ACTION_ICON_SIZE = 14
/** Lucide glyph size for the file-type icon in the card's leading slot. */
const FILE_ICON_SIZE = 20

// Renders a wiki file attachment: icon + filename + size, with hover actions
// (preview / download / delete). PDFs, self-contained HTML, Word documents,
// spreadsheets, delimited text, Markdown and plain text all get an expandable
// inline preview panel (PDF via an inline iframe src; the rest converted
// client-side into a sandboxed srcdoc — see wiki-file-preview-source.ts).
// Image attachments take a third path: the browser is already the renderer, so
// the card shows a real thumbnail in its leading slot and clicking it opens the
// full image in a lightbox — no expandable panel, no conversion.
// A type that has no inline renderer, or is too large for one, falls back to
// the new-tab preview when the backend will serve it inline, and to download
// otherwise. Action buttons use the FileActionButton helper below — see its
// comment for why they're <button>s, not <a>s.
export function WikiFileCard({ node, editor, getPos }: ReactNodeViewProps): ReactElement {
  const attrs = node.attrs as unknown as FileNodeAttrs
  const isEditable = editor.isEditable
  const url = attrs.url ?? ""
  const filename = attrs.filename || "file"
  // A card written as Markdown may only have guessed its type; the server's
  // answer replaces a guess of octet-stream (wiki-file-type-probe.ts).
  const contentType = useServerContentType(url, canonicalContentType(attrs.contentType))
  const canPreview = servedInline(contentType)
  // How it previews (file-preview/kind.ts). The expandable panel shows a PDF
  // (<iframe src=...?preview=1>), media (a native player), or a document we
  // fetched and converted ourselves into a sandboxed, CSP-constrained srcdoc.
  // Images bypass the panel entirely — see the thumbnail/lightbox path below.
  // "native" ones the browser draws from the URL; TIFF and HEIC are decoded
  // first (use-decoded-image.ts).
  const kind = filePreviewKind(contentType, filename, attrs.size)
  const { isPdf, mediaKind, renderedKind } = kind
  const imageFormat = url !== "" ? kind.imageFormat : null
  const isImage = imageFormat !== null
  const canPreviewInline = panelPreviews(kind) && url !== ""
  const previewUrl = url ? `${url}?preview=1` : ""

  // The frame is style-isolated, so the converters embed a palette rather than
  // inheriting the app's. Resolved (not raw) theme, so "system" maps to a real
  // value instead of leaking through as a string the converters don't know.
  const { resolvedTheme } = useTheme()
  const previewTheme = resolvedTheme === "dark" ? "dark" : "light"

  // Restore saved preview state (expanded + height) from localStorage.
  const fileId = attrs.fileId
  const savedPreview = canPreviewInline && fileId ? loadFilePreviewState(fileId) : null

  // Whether the inline preview panel is open. The frame is only mounted while
  // expanded, so collapsed cards never fetch the file bytes.
  const [expanded, setExpanded] = useState(savedPreview !== null)
  // Whether the image lightbox is open. Mounted only while open, so a document
  // full of image attachments pays nothing for the ones nobody opens.
  const [lightboxOpen, setLightboxOpen] = useState(false)
  // Set when the thumbnail fails to load — most often a blob the sweeper
  // collected, leaving the node behind. Falling back to the type icon keeps the
  // card readable instead of leaving a broken-image gap, and disarms the
  // lightbox, which would only show the same failure larger.
  const [thumbFailed, setThumbFailed] = useState(false)
  // The print page never scrolls, so a lazy thumbnail below the initial
  // viewport would still be unfetched when window.print() fires and export as a
  // blank tile. Same trade the image node makes.
  const isPrintMode = usePrintMode()
  // The card — what a decode waits to see near the viewport, and what the
  // preview panel scrolls into view when it opens.
  const cardRef = useRef<HTMLDivElement>(null)
  const decoded = useDecodedImage(
    imageFormat === "native" ? null : imageFormat,
    url,
    attrs.size,
    cardRef,
    isPrintMode,
  )
  // Until a decode finishes, the card shows its type icon, and a failed one
  // leaves it there.
  const imageSrc = imageFormat === "native" ? previewUrl : decoded.src
  const showThumbnail = isImage && !thumbFailed && imageSrc !== null
  // Same focus-restore scroll as the inline image node — see the hook.
  const captureFocusTarget = useFocusRestoreWithoutScroll(lightboxOpen)

  function openLightbox() {
    captureFocusTarget()
    setLightboxOpen(true)
  }
  // Preview height in px, or null for the CSS default. Restored from
  // localStorage if a saved state exists; otherwise computed on first open
  // by fitPreviewToViewport, and updated by the drag handle.
  const [previewHeight, setPreviewHeightRaw] = useState<number | null>(
    savedPreview?.height ?? null,
  )
  function setPreviewHeight(h: number | null) {
    setPreviewHeightRaw(h)
    if (fileId && h !== null) {
      saveFilePreviewState({ fileId, height: h })
    }
  }
  // The preview panel — target of the native Fullscreen request.
  const previewRef = useRef<HTMLDivElement>(null)
  // Set when Fullscreen is triggered from a collapsed card: the panel must
  // mount before we can request fullscreen on it, so we defer the request to
  // the effect below that fires once the panel is expanded.
  const pendingFullscreenRef = useRef(false)
  // Bytes are fetched and converted only while the panel is open.
  const rendered = useRenderedPreview(
    url,
    renderedKind,
    expanded && renderedKind !== null,
    previewTheme,
    filename,
  )

  function handleDelete() {
    const pos = typeof getPos === "function" ? getPos() : undefined
    if (pos == null) return
    editor
      .chain()
      .focus()
      .deleteRange({ from: pos, to: pos + node.nodeSize })
      .run()
  }

  function handleDownload() {
    if (!url) return
    triggerDownload(url, filename)
  }

  function handlePreview() {
    if (!previewUrl) return
    window.open(previewUrl, "_blank", "noopener,noreferrer")
  }

  const pendingScrollRef = useRef(false)
  // True when this reader opened the panel just now, as opposed to its being
  // restored open with the page. Only the first takes the pointer straight
  // away; see FilePreviewPanel.
  const [openedByReader, setOpenedByReader] = useState(false)

  function toggleInlinePreview() {
    if (!canPreviewInline) return
    const opening = !expanded
    if (opening) setOpenedByReader(true)
    if (opening && !pendingFullscreenRef.current) {
      pendingScrollRef.current = true
      if (previewHeight === null) {
        fitPreviewToViewport()
      }
    }
    setExpanded(opening)
    if (fileId) {
      if (opening) {
        // fitPreviewToViewport already saves when it sets the height; this
        // covers the reopen case where the height was carried over in state.
        if (previewHeight !== null) {
          saveFilePreviewState({ fileId, height: previewHeight })
        }
      } else {
        removeFilePreviewState(fileId)
      }
    }
  }

  // Set the preview height to fill the remaining viewport space below the card
  // header, so the operator sees the full preview without scrolling past it.
  // Only runs on first open when the user hasn't manually dragged a height.
  function fitPreviewToViewport() {
    const card = cardRef.current
    if (!card) return
    // Walk up to the nearest overflow container — the element whose visible
    // height bounds the preview. Unlike findScrollParent (which requires
    // scrollHeight > clientHeight), we accept one that doesn't scroll yet,
    // because the preview we're about to open is what will make it scroll.
    let scroller: HTMLElement | null = card.parentElement
    while (scroller) {
      const ov = getComputedStyle(scroller).overflowY
      if (ov === "auto" || ov === "scroll") break
      scroller = scroller.parentElement
    }
    const visibleHeight = scroller
      ? scroller.clientHeight
      : window.innerHeight
    // After scrollIntoView the card's top aligns with the scroller's top,
    // so available height = visible viewport − card height − panel chrome
    // (8px margin-top + 2px border + 12px resize handle).
    const cardHeight = card.getBoundingClientRect().height
    const overhead = 22
    const available = visibleHeight - cardHeight - overhead
    if (available > 160) {
      setPreviewHeight(Math.round(available))
    }
  }

  // Scroll the card into view once the preview panel has real content. For
  // converted formats this fires when `rendered.content` arrives; for PDFs and
  // media the panel mounts with its iframe/player immediately so the effect
  // fires on the `expanded` transition. A ResizeObserver on the card waits for
  // the layout to settle before scrolling — without it the scrollable area
  // hasn't grown yet when the card is near the bottom of the page.
  useEffect(() => {
    if (!pendingScrollRef.current) return
    if (!expanded) return
    if (renderedKind !== null && rendered.content === null && rendered.error === null) return

    pendingScrollRef.current = false
    const card = cardRef.current
    if (!card) return

    const observer = new ResizeObserver(() => {
      observer.disconnect()
      card.scrollIntoView({ behavior: "smooth", block: "start" })
    })
    observer.observe(card)

    return () => observer.disconnect()
  }, [expanded, rendered.content, rendered.error, renderedKind])

  // Maximize the preview via the native Fullscreen API. Toggles back out if
  // already on. When the card is collapsed we first open the panel and defer
  // the actual request to the effect below — the fullscreen target can't be
  // mounted until the panel renders, and the user activation survives the
  // extra render tick.
  function toggleFullscreen() {
    if (document.fullscreenElement) {
      void document.exitFullscreen()
      return
    }
    if (!expanded) {
      setOpenedByReader(true)
      pendingFullscreenRef.current = true
      setExpanded(true)
      return
    }
    void previewRef.current?.requestFullscreen().catch(() => {
      /* user denied or unsupported — leave the inline panel as-is */
    })
  }

  // Fire a deferred fullscreen request once the panel has mounted (see
  // toggleFullscreen). Runs only when the collapsed-card path armed it.
  useEffect(() => {
    if (!expanded || !pendingFullscreenRef.current) return
    pendingFullscreenRef.current = false
    void previewRef.current?.requestFullscreen().catch(() => {
      /* user denied or unsupported — leave the inline panel open */
    })
  }, [expanded])

  // Primary "open it" action for the filename row: expand the inline panel
  // for PDF/HTML, preview in a new tab for other safe types, otherwise
  // download.
  function handleFilenameClick() {
    if (showThumbnail) {
      openLightbox()
      return
    }
    if (canPreviewInline) {
      toggleInlinePreview()
      return
    }
    if (canPreview && url) {
      handlePreview()
      return
    }
    handleDownload()
  }

  return (
    <NodeViewWrapper className="wiki-file-wrapper" as="figure">
      <div ref={cardRef} className="wiki-file-card" contentEditable={false}>
        {showThumbnail ? (
          <button
            type="button"
            className="wiki-file-thumb"
            aria-label={`Open ${filename} full size`}
            title="Open full size"
            onMouseDown={swallow}
            onClick={openLightbox}
          >
            <img
              src={imageSrc ?? undefined}
              alt=""
              // Below-the-fold attachments must not fetch until scrolled to —
              // a long document can carry many of these, and the thumbnail is
              // the full-size image scaled down by the browser (there is no
              // separate thumbnail artifact on the backend).
              loading={isPrintMode ? "eager" : "lazy"}
              decoding={isPrintMode ? "sync" : "async"}
              draggable={false}
              onError={() => setThumbFailed(true)}
            />
          </button>
        ) : (
          <div className="wiki-file-icon" aria-hidden="true">
            <FileTypeIcon contentType={contentType} filename={filename} size={FILE_ICON_SIZE} />
          </div>
        )}
        <div className="wiki-file-meta">
          <button
            type="button"
            className="wiki-file-name"
            title={filename}
            onMouseDown={swallow}
            onClick={handleFilenameClick}
          >
            {filename}
          </button>
          <span className="wiki-file-size">{formatBytes(attrs.size)}</span>
        </div>
        <div className="wiki-file-actions">
          {showThumbnail ? (
            <FileActionButton
              icon={<Maximize2Icon size={ACTION_ICON_SIZE} />}
              label="Open image full size"
              title="Open full size"
              onClick={openLightbox}
            />
          ) : null}
          {canPreviewInline ? (
            <FileActionButton
              icon={expanded ? <ChevronDownIcon size={ACTION_ICON_SIZE} /> : <EyeIcon size={ACTION_ICON_SIZE} />}
              label={expanded ? "Hide preview" : "Preview file"}
              title={expanded ? "Hide preview" : "Preview"}
              expanded={expanded}
              onClick={toggleInlinePreview}
            />
          ) : null}
          {canPreviewInline ? (
            <FileActionButton
              icon={<Maximize2Icon size={ACTION_ICON_SIZE} />}
              label="View preview fullscreen"
              title="Fullscreen"
              onClick={toggleFullscreen}
            />
          ) : null}
          {canPreview && !canPreviewInline && !showThumbnail && url ? (
            <FileActionButton
              icon={<ExternalLinkIcon size={ACTION_ICON_SIZE} />}
              label="Preview file"
              title="Preview in new tab"
              onClick={handlePreview}
            />
          ) : null}
          {url ? (
            <FileActionButton
              icon={<DownloadIcon size={ACTION_ICON_SIZE} />}
              label="Download file"
              title="Download"
              onClick={handleDownload}
            />
          ) : null}
          {isEditable ? (
            <FileActionButton
              icon={<Trash2Icon size={ACTION_ICON_SIZE} />}
              label="Remove file attachment"
              title="Delete"
              danger
              onClick={handleDelete}
            />
          ) : null}
        </div>
      </div>
      {lightboxOpen ? (
        <Lightbox
          open
          close={() => setLightboxOpen(false)}
          slides={[{ src: imageSrc ?? previewUrl, alt: filename }]}
          plugins={[Zoom, Fullscreen, Counter]}
          carousel={{ finite: true }}
          controller={{ closeOnBackdropClick: true }}
          render={{
            // One attachment per card — no carousel to page through.
            buttonPrev: () => null,
            buttonNext: () => null,
          }}
          zoom={{ maxZoomPixelRatio: 4, scrollToZoom: true }}
        />
      ) : null}
      {canPreviewInline && expanded ? (
        <FilePreviewPanel
          containerRef={previewRef}
          isPdf={isPdf}
          mediaKind={mediaKind}
          previewUrl={previewUrl}
          filename={filename}
          rendered={rendered}
          height={previewHeight}
          onHeightChange={setPreviewHeight}
          startActive={openedByReader}
        />
      ) : null}
    </NodeViewWrapper>
  )
}

interface FileActionButtonProps {
  icon: ReactElement
  /** Accessible name; also the screen-reader label. */
  label: string
  /** Hover tooltip. */
  title: string
  onClick: () => void
  /** Renders the destructive (red-hover) variant. */
  danger?: boolean
  /** When set, exposes aria-expanded for the inline-preview toggle. */
  expanded?: boolean
}

// Shared hover-action button for the card. <button> (not <a>) because anchor
// clicks inside a ProseMirror node view double-fire under React StrictMode;
// mousedown is swallowed so PM can't start a selection cycle against it.
function FileActionButton({
  icon,
  label,
  title,
  onClick,
  danger,
  expanded,
}: FileActionButtonProps): ReactElement {
  return (
    <button
      type="button"
      className={
        danger
          ? "wiki-file-action-button wiki-file-action-button--danger"
          : "wiki-file-action-button"
      }
      aria-label={label}
      aria-expanded={expanded}
      title={title}
      onMouseDown={swallow}
      onClick={onClick}
    >
      {icon}
    </button>
  )
}

function swallow(e: ReactMouseEvent): void {
  e.preventDefault()
  e.stopPropagation()
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB"]
  let value = bytes
  let idx = 0
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024
    idx++
  }
  const precision = idx === 0 || value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(precision)} ${units[idx]}`
}

// Anchor is appended to <body> — outside the editor's DOM subtree — so PM's
// click handling can't touch it. Guarantees exactly one download per call.
function triggerDownload(url: string, filename: string): void {
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.rel = "noopener noreferrer"
  a.style.display = "none"
  document.body.appendChild(a)
  try {
    a.click()
  } finally {
    document.body.removeChild(a)
  }
}

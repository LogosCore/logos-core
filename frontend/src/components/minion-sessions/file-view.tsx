import { useCallback, useState, type ReactNode } from "react"
import { CheckIcon, CopyIcon, DownloadIcon, EyeIcon, LoaderIcon, XIcon } from "lucide-react"
import { FilePreviewPane } from "@/components/file-preview/file-preview-pane"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import { canonicalContentType, filePreviewKind, liveKind, panelPreviews, type FilePreviewKind } from "@/components/file-preview/kind"
import { fileTypeName } from "@/components/file-preview/type-name"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { DEFAULT_STRINGS_FILTER, type MinionFileStringsFilter } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { FileHexView } from "./file-hex-view"
import { FileStringsView } from "./file-strings-view"
import { LiveTextPane } from "./live-text-pane"
import { hexPlaceOf, type HexPlace } from "./hex"
import { TabButton } from "./tab-button"
import { savedName } from "./verify-commands"
import { VerifyHashCard } from "./verify-hash-card"
import { useByteBlocks } from "./use-byte-blocks"

// A file_download task's file, or a screenshot task's picture (docs:
// contracts/task-results-and-actions.md, file_download and screenshot), in
// four views: Overview (what it is, how much of it came, whether it checked
// out, its download), Preview (the wiki's renderers, for a type they show),
// Hex (its bytes) and Strings (the text in them). Live: it follows the
// task's updates as the chunks come, and its views open before its end, on
// what has come: a picture drawn row by row as its bytes come, text that
// grows, the bytes and strings so far, a download that starts at once
// (core reads it as it comes: ?follow=1, ranges of what came). Nothing is
// checked before the end. Where the hex view is and the strings' filter
// stay while another view shows.

const API_URL = import.meta.env.VITE_API_URL as string

type File = NonNullable<MinionTaskFieldsFragment["file"]>
type View = "overview" | "preview" | "hex" | "strings"

const STATE: Record<string, { label: string; className: string }> = {
  RECEIVING: { label: "Coming", className: "text-sky-600 dark:text-sky-400" },
  JOINING: { label: "Checking", className: "text-sky-600 dark:text-sky-400" },
  COMPLETE: { label: "Kept", className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { label: "Failed", className: "text-red-600 dark:text-red-400" },
  CANCELLED: { label: "Cancelled", className: "text-muted-foreground" },
}

/** A path's last part, by either separator: the minion's host may be either kind. */
function baseName(path: string): string {
  return path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1) || path
}

export function FileView({
  file: f,
  end,
}: {
  file: File
  /** At the tab row's end, after every view's tools: a dialog's close. */
  end?: ReactNode
}) {
  const url = `${API_URL}/minion-files/${encodeURIComponent(f.id)}`
  const name = baseName(f.name)
  const contentType = canonicalContentType(f.contentType) || "application/octet-stream"
  const kept = f.state === "COMPLETE"
  const coming = f.state === "RECEIVING" || f.state === "JOINING"
  const kind = filePreviewKind(contentType, name, f.size)
  // How much of it there is to read, and what of it shows as it comes.
  const available = kept ? f.size : coming ? f.received : 0
  const live = coming && f.received > 0 ? liveKind(kind, contentType) : null
  const followUrl = `${url}?follow=1`
  // A file with a preview opens on it, first of the views; the others on
  // their overview. Until the operator picks one, the view follows the
  // file: a download that is kept while shown turns to its preview, and a
  // picture coming shows from its first chunk.
  const views: View[] = [
    ...((kept && (panelPreviews(kind) || kind.imageFormat !== null)) || live !== null ? (["preview"] as const) : []),
    "overview",
    ...(available > 0 ? (["hex", "strings"] as const) : []),
  ]
  const [picked, setPicked] = useState<View | null>(null)
  const view = picked !== null && views.includes(picked) ? picked : views[0]

  const bytes = useByteBlocks(url, f.size, available)
  const [hexPlace, setHexPlace] = useState<HexPlace>({ top: 0, sel: null })
  const onHexPlace = useCallback((update: (p: HexPlace) => HexPlace) => setHexPlace(update), [])
  const [stringsFilter, setStringsFilter] = useState<MinionFileStringsFilter>(DEFAULT_STRINGS_FILTER)

  const LABEL: Record<View, string> = { overview: "Overview", preview: "Preview", hex: "Hex", strings: "Strings" }
  // The file's name beside the tabs, in every view: which file it is stays
  // in sight.
  const toolbar = (tools: ReactNode) => (
    <div className="flex min-h-8 flex-wrap items-center gap-x-2 gap-y-1 border-b px-2 py-1">
      <div className="flex shrink-0 items-center gap-1">
        {views.map((v) => (
          <TabButton key={v} active={view === v} onClick={() => setPicked(v)}>
            {LABEL[v]}
          </TabButton>
        ))}
      </div>
      <span className="flex min-w-0 flex-1 items-baseline gap-1.5 px-1" title={f.name}>
        <FileTypeIcon contentType={contentType} filename={name} className="size-3.5 shrink-0 self-center text-muted-foreground" />
        <span className="truncate font-mono text-xs font-medium">{name}</span>
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatBytes(f.size)}</span>
        {coming && (
          <span className="shrink-0 text-xs text-sky-600 dark:text-sky-400" title="Shown as it comes: its size and SHA-256 are checked at its end">
            coming, not checked yet
          </span>
        )}
      </span>
      {(tools || end) && (
        <div className="ml-auto flex items-center gap-1">
          {tools}
          {end}
        </div>
      )}
    </div>
  )
  const download = (
    <Button
      variant="ghost"
      size="icon-xs"
      title={coming ? `Download ${name} as it comes` : `Download ${name}`}
      aria-label="Download the file"
      nativeButton={false}
      render={<a href={coming ? followUrl : url} download />}
    >
      <DownloadIcon />
    </Button>
  )

  if (view === "preview") {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {toolbar(download)}
        {kept ? (
          <FilePreviewPane url={url} kind={kind} filename={name} size={f.size} />
        ) : live === "text" ? (
          <LiveTextPane key={followUrl} url={followUrl} />
        ) : (
          <FilePreviewPane url={followUrl} kind={kind} filename={name} size={f.size} />
        )}
      </div>
    )
  }
  if (view === "hex") {
    return <FileHexView url={url} size={f.size} available={available} bytes={bytes} place={hexPlace} onPlace={onHexPlace} toolbar={toolbar} />
  }
  if (view === "strings") {
    return (
      <FileStringsView
        fileId={f.id}
        size={f.size}
        available={available}
        filter={stringsFilter}
        onFilter={setStringsFilter}
        onShowInHex={(offset, length) => {
          setHexPlace(hexPlaceOf(offset, length))
          setPicked("hex")
        }}
        toolbar={toolbar}
      />
    )
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {toolbar(null)}
      <div className="flex min-h-0 flex-1 items-start overflow-auto">
        <FileOverview
          file={f}
          name={name}
          contentType={f.contentType ? contentType : null}
          url={url}
          thumbnail={kept && (f.thumb || f.size <= THUMBNAIL_MAX) ? thumbnailOf(kind) : null}
          onPreview={views.includes("preview") ? () => setPicked("preview") : undefined}
        />
      </div>
    </div>
  )
}

// What the file is and where it stands, from the pane's top left (its name
// is in the tab row above): what can be done with it (or how much came, or
// why it ended), then its details and, once kept, the commands that verify
// a download, side by side where the pane is wide. Nothing is cut: the path,
// the hash and the commands wrap.
function FileOverview({
  file: f,
  name,
  contentType,
  url,
  thumbnail,
  onPreview,
}: {
  file: File
  name: string
  contentType: string | null
  url: string
  /** A picture or a video's first frame, beside the actions. */
  thumbnail: "image" | "video" | null
  /** Shows the Preview tab; absent when the file has none. */
  onPreview?: () => void
}) {
  const state = STATE[f.state] ?? { label: f.state.toLowerCase(), className: "text-muted-foreground" }
  const coming = f.state === "RECEIVING" || f.state === "JOINING"
  const pct = f.size > 0 ? Math.min(100, Math.round((f.received / f.size) * 100)) : 100
  const Icon = f.state === "COMPLETE" ? CheckIcon : f.state === "FAILED" || f.state === "CANCELLED" ? XIcon : LoaderIcon
  const typeName = fileTypeName(contentType ?? "", name)
  const sha256 = f.sha256 ?? null
  // A screenshot's picture is on no file system: its name, its size in
  // pixels and its screen, said by its task's data at its end.
  const shot = f.kind === "screenshot"
  const pixels = f.width != null && f.height != null ? `${f.width} × ${f.height}` : null
  return (
    // Columns follow the pane's width, not the window's.
    <div className="@container flex w-full max-w-6xl flex-col gap-4 p-4">
      {f.state === "COMPLETE" ? (
        <div className="flex flex-wrap items-center gap-2">
          {thumbnail && <Thumbnail url={url} kind={thumbnail} small={f.thumb} onClick={onPreview} />}
          <Button nativeButton={false} render={<a href={url} download />}>
            <DownloadIcon />
            Download
          </Button>
          {onPreview && (
            <Button variant="outline" onClick={onPreview}>
              <EyeIcon />
              Preview
            </Button>
          )}
          {sha256 && (
            <Button variant="outline" onClick={() => void copyToClipboard(sha256, "SHA-256")}>
              <CopyIcon />
              Copy SHA-256
            </Button>
          )}
        </div>
      ) : (
        <div className="flex w-full max-w-md flex-col gap-2">
          <div className={cn("inline-flex items-center gap-1.5 text-sm", state.className)}>
            <Icon className={cn("size-4", coming && "animate-spin")} />
            {state.label}
            {coming && (
              <span className="text-xs text-muted-foreground tabular-nums">
                {formatBytes(f.received)} of {formatBytes(f.size)} ({pct}%)
              </span>
            )}
          </div>
          {coming && (
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
              <div className="h-full bg-sky-500 transition-[width]" style={{ width: `${pct}%` }} />
            </div>
          )}
          {f.err && <p className="text-xs text-red-600 dark:text-red-400">{f.err}</p>}
          {coming && (
            <div>
              <Button variant="outline" nativeButton={false} render={<a href={`${url}?follow=1`} download />}>
                <DownloadIcon />
                Download as it comes
              </Button>
            </div>
          )}
        </div>
      )}

      <div className="grid items-start gap-4 @3xl:grid-cols-2">
        <InfoCard labelWidth="w-24">
          {shot ? (
            <>
              <LabelRow label="Name">
                <span className="font-mono text-xs break-all">{f.name}</span>
              </LabelRow>
              <LabelRow label="Picture">
                {pixels ? (
                  <span className="tabular-nums">{pixels} pixels</span>
                ) : (
                  <span className="text-muted-foreground">said when the task ends</span>
                )}
              </LabelRow>
              <LabelRow label="Screen">
                {f.display ? (
                  <span className="font-mono text-xs break-all">{f.display}</span>
                ) : (
                  <span className="text-muted-foreground">{pixels ? "every screen" : "said when the task ends"}</span>
                )}
              </LabelRow>
            </>
          ) : (
            <LabelRow label="Path">
              <span className="font-mono text-xs break-all">{f.name}</span>
            </LabelRow>
          )}
          <LabelRow label="Size">
            <span className="tabular-nums">{f.size.toLocaleString("en-US")} bytes</span>
            {f.size >= 1024 && <span className="text-muted-foreground tabular-nums"> · {formatBytes(f.size)}</span>}
          </LabelRow>
          <LabelRow label="Type">
            {contentType ? (
              <>
                <span className="break-all">{contentType}</span>
                {typeName && <span className="text-muted-foreground"> · {typeName}</span>}
              </>
            ) : (
              <span className="text-muted-foreground">{f.state === "COMPLETE" ? "not detected" : "known once kept"}</span>
            )}
          </LabelRow>
          <LabelRow label="SHA-256">
            {sha256 ? (
              <code className="font-mono text-xs break-all">{sha256}</code>
            ) : (
              <span className="text-muted-foreground">said with the last chunk</span>
            )}
          </LabelRow>
          <LabelRow label="State">
            <span className={state.className}>{state.label}</span>
            {f.doneAt && <span className="text-muted-foreground tabular-nums"> · {formatDateTime(f.doneAt)}</span>}
          </LabelRow>
        </InfoCard>
        {f.state === "COMPLETE" && sha256 && <VerifyHashCard name={savedName(f.name, `file-${f.id.slice(0, 8)}`)} sha256={sha256} />}
      </div>
    </div>
  )
}

// Pictures the browser draws itself, up to this size, show in the overview
// (a big one is the Preview's to load); a video, its first frame, which
// costs only its start.
const THUMBNAIL_MAX = 25 * 1024 * 1024

function thumbnailOf(kind: FilePreviewKind): "image" | "video" | null {
  if (kind.imageFormat === "native") return "image"
  if (kind.mediaKind === "video") return "video"
  return null
}

/**
 * A picture's or video's thumbnail beside the overview's actions; a click
 * previews it. small: core keeps a thumbnail of it (a screenshot's), read
 * instead of the whole picture.
 */
function Thumbnail({ url, kind, small, onClick }: { url: string; kind: "image" | "video"; small?: boolean; onClick?: () => void }) {
  const [failed, setFailed] = useState(false)
  const inline = small && kind === "image" ? `${url}?thumb=1` : `${url}?preview=1`
  if (failed) return null
  return (
    <button
      type="button"
      className="size-9 shrink-0 cursor-pointer overflow-hidden rounded-md bg-muted ring-foreground/10 hover:ring-2"
      title="Preview"
      onClick={onClick}
    >
      {kind === "image" ? (
        <img src={inline} alt="" className="size-full object-cover" draggable={false} onError={() => setFailed(true)} />
      ) : (
        // The first frame, a moment in: at 0 some videos are still black.
        <video src={`${inline}#t=0.1`} preload="metadata" muted playsInline className="size-full object-cover" onError={() => setFailed(true)} />
      )}
    </button>
  )
}

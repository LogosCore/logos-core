import { useCallback, useState, type ReactNode } from "react"
import { CheckIcon, CopyIcon, DownloadIcon, EyeIcon, LoaderIcon, XIcon } from "lucide-react"
import { FilePreviewPane } from "@/components/file-preview/file-preview-pane"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import { canonicalContentType, filePreviewKind, panelPreviews, type FilePreviewKind } from "@/components/file-preview/kind"
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
import { hexPlaceOf, type HexPlace } from "./hex"
import { TabButton } from "./tab-button"
import { savedName } from "./verify-commands"
import { VerifyHashCard } from "./verify-hash-card"
import { useByteBlocks } from "./use-byte-blocks"

// A file_download task's file (docs: contracts/task-results-and-actions.md,
// file_download), in four views: Overview (what it is, how much of it came,
// whether it checked out, its download), Preview (the wiki's renderers, for
// a type they show), Hex (its bytes) and Strings (the text in them). Live:
// it follows the task's updates as the chunks come; the other views open
// once it is kept. Where the hex view is and the strings' filter stay while
// another view shows.

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

export function FileView({ file: f }: { file: File }) {
  const url = `${API_URL}/minion-files/${encodeURIComponent(f.id)}`
  const name = baseName(f.name)
  const contentType = canonicalContentType(f.contentType) || "application/octet-stream"
  const kept = f.state === "COMPLETE"
  const kind = filePreviewKind(contentType, name, f.size)
  const views: View[] = [
    "overview",
    ...(kept && (panelPreviews(kind) || kind.imageFormat !== null) ? (["preview"] as const) : []),
    ...(kept && f.size > 0 ? (["hex", "strings"] as const) : []),
  ]
  const [picked, setPicked] = useState<View>("overview")
  const view = views.includes(picked) ? picked : "overview"

  const bytes = useByteBlocks(url, f.size)
  const [hexPlace, setHexPlace] = useState<HexPlace>({ top: 0, sel: null })
  const onHexPlace = useCallback((update: (p: HexPlace) => HexPlace) => setHexPlace(update), [])
  const [stringsFilter, setStringsFilter] = useState<MinionFileStringsFilter>(DEFAULT_STRINGS_FILTER)

  const LABEL: Record<View, string> = { overview: "Overview", preview: "Preview", hex: "Hex", strings: "Strings" }
  const toolbar = (tools: ReactNode) => (
    <div className="flex min-h-8 flex-wrap items-center gap-x-2 gap-y-1 border-b px-2 py-1">
      <div className="flex shrink-0 items-center gap-1">
        {views.map((v) => (
          <TabButton key={v} active={view === v} onClick={() => setPicked(v)}>
            {LABEL[v]}
          </TabButton>
        ))}
      </div>
      {tools && <div className="ml-auto flex items-center gap-1">{tools}</div>}
    </div>
  )
  const download = (
    <Button variant="ghost" size="icon-xs" title={`Download ${name}`} aria-label="Download the file" render={<a href={url} download />}>
      <DownloadIcon />
    </Button>
  )

  if (view === "preview") {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {toolbar(download)}
        <FilePreviewPane url={url} kind={kind} filename={name} size={f.size} />
      </div>
    )
  }
  if (view === "hex") {
    return <FileHexView url={url} size={f.size} bytes={bytes} place={hexPlace} onPlace={onHexPlace} toolbar={toolbar} />
  }
  if (view === "strings") {
    return (
      <FileStringsView
        fileId={f.id}
        size={f.size}
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
      <div className="flex min-h-0 flex-1 overflow-auto">
        <FileOverview
          file={f}
          name={name}
          contentType={f.contentType ? contentType : null}
          url={url}
          thumbnail={kept && f.size <= THUMBNAIL_MAX ? thumbnailOf(kind) : null}
          onPreview={views.includes("preview") ? () => setPicked("preview") : undefined}
        />
      </div>
    </div>
  )
}

// What the file is and where it stands, in the middle of the pane, in two
// columns where it is wide: its kind's icon, name, size and kind in words,
// the download once kept (or how much came, or why it ended); its details
// beside them. One column, the details under, where it is narrow.
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
  /** A picture or a video's first frame in place of the kind's icon. */
  thumbnail: "image" | "video" | null
  /** Shows the Preview tab; absent when the file has none. */
  onPreview?: () => void
}) {
  const state = STATE[f.state] ?? { label: f.state.toLowerCase(), className: "text-muted-foreground" }
  const coming = f.state === "RECEIVING" || f.state === "JOINING"
  const pct = f.size > 0 ? Math.min(100, Math.round((f.received / f.size) * 100)) : 100
  const Icon = f.state === "COMPLETE" ? CheckIcon : f.state === "FAILED" || f.state === "CANCELLED" ? XIcon : LoaderIcon
  const typeName = fileTypeName(contentType ?? "", name)
  return (
    // m-auto centres it both ways, and lets it scroll from its top when the
    // pane is the shorter. Columns follow the pane's width, not the window's.
    <div className="@container m-auto w-full max-w-5xl px-6 py-6">
      <div className="grid items-center gap-6 @2xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] @2xl:gap-10">
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <Thumbnail url={url} kind={thumbnail} contentType={contentType ?? ""} name={name} onClick={onPreview} />
            <div className="flex min-w-0 flex-col gap-0.5">
              <h3 className="font-mono text-base font-medium break-all">{name}</h3>
              <p className="text-xs text-muted-foreground tabular-nums">
                {formatBytes(f.size)}
                {typeName && <> · {typeName}</>}
              </p>
            </div>
          </div>

          {f.state === "COMPLETE" ? (
            <div className="flex flex-wrap gap-2">
              <Button render={<a href={url} download />}>
                <DownloadIcon />
                Download
              </Button>
              {onPreview && (
                <Button variant="outline" onClick={onPreview}>
                  <EyeIcon />
                  Preview
                </Button>
              )}
            </div>
          ) : (
            <div className="flex w-full max-w-sm flex-col gap-2">
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
            </div>
          )}
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          <InfoCard labelWidth="w-24">
            <LabelRow label="Path">
              <span className="font-mono text-xs break-all">{f.name}</span>
            </LabelRow>
            <LabelRow label="Size">
              <span className="tabular-nums">{f.size.toLocaleString("en-US")} bytes</span>
            </LabelRow>
            <LabelRow label="Type">
              {contentType ? (
                <span className="block truncate" title={contentType}>
                  {contentType}
                </span>
              ) : (
                <span className="text-muted-foreground">{f.state === "COMPLETE" ? "not detected" : "known once kept"}</span>
              )}
            </LabelRow>
            <LabelRow label="SHA-256">
              {f.sha256 ? (
                <span className="flex min-w-0 items-center gap-1">
                  <code className="min-w-0 flex-1 truncate font-mono text-xs" title={f.sha256}>
                    {f.sha256}
                  </code>
                  <Button variant="ghost" size="icon-xs" className="-my-1" title="Copy the SHA-256" aria-label="Copy the SHA-256"
                    onClick={() => void copyToClipboard(f.sha256!, "SHA-256")}>
                    <CopyIcon />
                  </Button>
                </span>
              ) : (
                <span className="text-muted-foreground">said with the last chunk</span>
              )}
            </LabelRow>
            <LabelRow label="State">
              <span className={state.className}>{state.label}</span>
              {f.doneAt && <span className="text-muted-foreground tabular-nums"> · {formatDateTime(f.doneAt)}</span>}
            </LabelRow>
          </InfoCard>
          {f.state === "COMPLETE" && f.sha256 && <VerifyHashCard name={savedName(f.name, `file-${f.id.slice(0, 8)}`)} sha256={f.sha256} />}
        </div>
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

/** The overview's picture of the file: a thumbnail of a picture or video, else its kind's icon; a click previews it. */
function Thumbnail({
  url,
  kind,
  contentType,
  name,
  onClick,
}: {
  url: string
  kind: "image" | "video" | null
  contentType: string
  name: string
  onClick?: () => void
}) {
  const [failed, setFailed] = useState(false)
  const inline = `${url}?preview=1`
  const shown = !failed ? kind : null
  const box = "flex shrink-0 items-center justify-center overflow-hidden rounded-2xl bg-muted text-muted-foreground"
  if (shown === null)
    return (
      <div className={cn(box, "size-14")}>
        <FileTypeIcon contentType={contentType} filename={name} size={26} />
      </div>
    )
  return (
    <button type="button" className={cn(box, "size-20 cursor-pointer ring-foreground/10 hover:ring-2")} title="Preview" onClick={onClick}>
      {shown === "image" ? (
        <img src={inline} alt="" className="size-full object-cover" draggable={false} onError={() => setFailed(true)} />
      ) : (
        // The first frame, a moment in: at 0 some videos are still black.
        <video src={`${inline}#t=0.1`} preload="metadata" muted playsInline className="size-full object-cover" onError={() => setFailed(true)} />
      )}
    </button>
  )
}

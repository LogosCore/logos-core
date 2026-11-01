import { useState } from "react"
import { ArrowDownToLineIcon, CheckIcon, CopyIcon, DownloadIcon, LoaderIcon, PauseIcon, TerminalIcon, XIcon } from "lucide-react"
import { toast } from "sonner"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import { fileTypeName } from "@/components/file-preview/type-name"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { fetchMinionStreamLink } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { savedName, shellQuote } from "./verify-commands"
import { VerifyHashCard } from "./verify-hash-card"

// A stream_file task's stream (docs: contracts/task-results-and-actions.md,
// file_stream), laid out as a kept file's Overview: the file on the host,
// Download (the browser takes the stream, resumable) and Copy curl (a
// signed link, which resumes when run again), and how far the downloader
// is. Live: it follows the task's updates as the downloader goes on.

const API_URL = import.meta.env.VITE_API_URL as string

type Task = MinionTaskFieldsFragment
type Stream = NonNullable<Task["stream"]>

const STATE: Record<string, { label: string; className: string }> = {
  WAITING: { label: "Waiting for a downloader", className: "text-muted-foreground" },
  STREAMING: { label: "Streaming", className: "text-sky-600 dark:text-sky-400" },
  PAUSED: { label: "Paused: no downloader", className: "text-amber-600 dark:text-amber-400" },
  DONE: { label: "Downloaded", className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { label: "Failed", className: "text-red-600 dark:text-red-400" },
  CANCELLED: { label: "Stopped", className: "text-muted-foreground" },
}

/** A path's last part, by either separator. */
function baseName(path: string): string {
  return path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1) || path
}

/** The downloader's rate since it took the stream, bytes a second. */
function rate(st: Stream): number | null {
  if (!st.attachedAt || !st.readerAt) return null
  const secs = (Date.parse(st.readerAt) - Date.parse(st.attachedAt)) / 1000
  return secs > 0 && st.delivered > st.from ? (st.delivered - st.from) / secs : null
}

function duration(secs: number): string {
  if (secs < 60) return `${Math.max(1, Math.round(secs))}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m ${Math.round(secs % 60)}s`
  return `${Math.floor(secs / 3600)}h ${Math.round((secs % 3600) / 60)}m`
}

export function StreamView({ task: t, stream: st }: { task: Task; stream: Stream }) {
  const state = STATE[st.state] ?? { label: st.state.toLowerCase(), className: "text-muted-foreground" }
  const open = st.state === "WAITING" || st.state === "STREAMING" || st.state === "PAUSED"
  const pct = st.size > 0 ? Math.min(100, Math.floor((st.delivered / st.size) * 100)) : 100
  const bps = st.state === "STREAMING" ? rate(st) : null
  const name = baseName(st.name)
  // What core's download saves it as: curl's -o, and what the checks name.
  const saved = savedName(st.name, `stream-${t.id.slice(0, 8)}`)
  const typeName = fileTypeName("", name)
  const url = `${API_URL}/minion-streams/${encodeURIComponent(t.id)}`
  const [copying, setCopying] = useState(false)
  const Icon =
    st.state === "DONE" ? CheckIcon : st.state === "PAUSED" ? PauseIcon : st.state === "STREAMING" ? LoaderIcon : st.state === "WAITING" ? ArrowDownToLineIcon : XIcon

  const copyCurl = async () => {
    setCopying(true)
    try {
      const path = await fetchMinionStreamLink(t.id)
      const link = new URL(`${API_URL}${path}`, window.location.origin).toString()
      await copyToClipboard(`curl -f -C - -o ${shellQuote(saved)} ${shellQuote(link)}`, "curl command")
    } catch (err) {
      toast.error(errorMessage(err, "Could not make a link"))
    } finally {
      setCopying(false)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 overflow-auto">
      {/* m-auto centres it both ways, and lets it scroll from its top when
          the pane is the shorter. Columns follow the pane's width. */}
      <div className="@container m-auto w-full max-w-5xl px-6 py-6">
        <div className="grid items-center gap-6 @2xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] @2xl:gap-10">
          <div className="flex min-w-0 flex-col gap-4">
            <div className="flex min-w-0 items-center gap-3">
              <div className="flex size-14 shrink-0 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
                <FileTypeIcon contentType="" filename={name} size={26} />
              </div>
              <div className="flex min-w-0 flex-col gap-0.5">
                <h3 className="font-mono text-sm font-medium break-all">{name}</h3>
                <p className="text-xs text-muted-foreground tabular-nums">
                  {formatBytes(st.size)}
                  {typeName && <> · {typeName}</>}
                </p>
              </div>
            </div>

            {open && (
              <div className="flex flex-wrap gap-2">
                <Button nativeButton={false} render={<a href={url} download />} title="Download it in this browser: it comes as the minion sends it, and resumes">
                  <DownloadIcon />
                  Download
                </Button>
                <Button variant="outline" disabled={copying} onClick={() => void copyCurl()}
                  title="A curl command with a signed link: it resumes when run again (24 hours at most)">
                  <TerminalIcon />
                  Copy curl
                </Button>
              </div>
            )}

            <div className="flex w-full max-w-sm flex-col gap-2">
              <div className={cn("inline-flex flex-wrap items-center gap-x-1.5 text-sm", state.className)}>
                <Icon className={cn("size-4", st.state === "STREAMING" && "animate-spin")} />
                {state.label}
                {(st.state === "STREAMING" || st.state === "PAUSED") && (
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {formatBytes(st.delivered)} of {formatBytes(st.size)} ({pct}%)
                    {bps !== null && (
                      <>
                        {" "}· {formatBytes(bps)}/s · {duration((st.size - st.delivered) / bps)} left
                      </>
                    )}
                  </span>
                )}
              </div>
              {(st.state === "STREAMING" || st.state === "PAUSED") && (
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
                  <div className={cn("h-full transition-[width]", st.state === "PAUSED" ? "bg-amber-500" : "bg-sky-500")} style={{ width: `${pct}%` }} />
                </div>
              )}
              {st.state === "WAITING" && (
                <p className="text-xs text-muted-foreground">
                  The minion holds the file open. Nothing is kept in core: it streams as it is downloaded. With no downloader for an hour, the stream stops.
                </p>
              )}
              {st.state === "FAILED" && t.err && <p className="text-xs text-red-600 dark:text-red-400">{t.err}</p>}
            </div>
          </div>

          <div className="flex min-w-0 flex-col gap-4">
            <InfoCard labelWidth="w-24">
              <LabelRow label="Path">
                <span className="font-mono text-xs break-all">{st.name}</span>
              </LabelRow>
              <LabelRow label="Size">
                <span className="tabular-nums">{st.size.toLocaleString("en-US")} bytes</span>
              </LabelRow>
              <LabelRow label="SHA-256">
                {st.sha256 ? (
                  <span className="flex min-w-0 items-center gap-1">
                    <code className="min-w-0 flex-1 truncate font-mono text-xs" title={st.sha256}>
                      {st.sha256}
                    </code>
                    <Button variant="ghost" size="icon-xs" className="-my-1" title="Copy the SHA-256" aria-label="Copy the SHA-256"
                      onClick={() => void copyToClipboard(st.sha256 ?? "", "SHA-256")}>
                      <CopyIcon />
                    </Button>
                  </span>
                ) : (
                  <span className="text-muted-foreground">once the minion has read it whole, in order</span>
                )}
              </LabelRow>
              <LabelRow label="State">
                <span className={state.className}>{state.label}</span>
              </LabelRow>
              <LabelRow label="Announced">
                <span className="tabular-nums">{formatDateTime(st.announcedAt)}</span>
              </LabelRow>
              {t.doneAt && (
                <LabelRow label="Ended">
                  <span className="tabular-nums">{formatDateTime(t.doneAt)}</span>
                </LabelRow>
              )}
            </InfoCard>
            {st.sha256 && <VerifyHashCard name={saved} sha256={st.sha256} />}
          </div>
        </div>
      </div>
    </div>
  )
}

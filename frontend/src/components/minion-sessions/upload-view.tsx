import { ArrowRightIcon, CheckIcon, CopyIcon, LoaderIcon, XIcon } from "lucide-react"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import { fileTypeName } from "@/components/file-preview/type-name"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { StreamUploader } from "./stream-uploader"

// A file_upload task's file going to its minion (docs:
// contracts/task-results-and-actions.md, file_upload), laid out as a kept
// file's Overview: what goes where, how far the minion has it (its asks
// for chunks: the bar, the rate, the time left), and at the end whether
// what it wrote is the file. Live: it follows the task's updates, one with
// each chunk the minion asks for.

type Task = MinionTaskFieldsFragment
type Upload = NonNullable<Task["upload"]>

const STATE: Record<string, { label: string; className: string }> = {
  WAITING: { label: "Waiting for the minion", className: "text-muted-foreground" },
  SENDING: { label: "Sending", className: "text-sky-600 dark:text-sky-400" },
  SENT: { label: "Sent: the minion checks it", className: "text-sky-600 dark:text-sky-400" },
  DONE: { label: "Written and checked", className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { label: "Failed", className: "text-red-600 dark:text-red-400" },
  CANCELLED: { label: "Cancelled", className: "text-muted-foreground" },
}

/** Where the file went: the data's path once it came, the path arg until then. */
function destination(t: Task, data: Record<string, unknown> | null): string | null {
  if (typeof data?.path === "string") return data.path
  const arg = (t.args as Record<string, unknown> | null)?.path
  return typeof arg === "string" ? arg : null
}

/** Bytes a second, from the first ask to the last; null before two. */
function rate(up: Upload): number | null {
  if (!up.firstAckAt || !up.lastAckAt) return null
  const secs = (Date.parse(up.lastAckAt) - Date.parse(up.firstAckAt)) / 1000
  return secs > 0 && up.acked > 0 ? up.acked / secs : null
}

function duration(secs: number): string {
  if (secs < 60) return `${Math.max(1, Math.round(secs))}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m ${Math.round(secs % 60)}s`
  return `${Math.floor(secs / 3600)}h ${Math.round((secs % 3600) / 60)}m`
}

export function UploadView({ task: t, data }: { task: Task; data: Record<string, unknown> | null }) {
  const up = t.upload!
  // A stream goes as its uploader sends it: before one came it waits for
  // the file, and after it went away it is paused. While one sends, the
  // minion catching up with it (all core has, taken) is just sending: the
  // upload is the slower side.
  const uploaderOff = up.stream && up.state === "SENDING" && up.uploaderState !== "UPLOADING" && up.uploaderState !== "DONE"
  const state = !uploaderOff
    ? (STATE[up.state] ?? { label: up.state.toLowerCase(), className: "text-muted-foreground" })
    : up.uploaderState === "PAUSED"
      ? { label: "Paused: its uploader went away", className: "text-amber-600 dark:text-amber-400" }
      : { label: "Waiting for the file", className: "text-muted-foreground" }
  const going = up.state === "WAITING" || up.state === "SENDING" || up.state === "SENT"
  const pct = !up.sized ? 0 : up.size > 0 ? Math.min(100, Math.floor((up.acked / up.size) * 100)) : 100
  const Icon = up.state === "DONE" ? CheckIcon : up.state === "FAILED" || up.state === "CANCELLED" ? XIcon : LoaderIcon
  const bps = rate(up)
  const dest = destination(t, data)
  const typeName = fileTypeName("", up.name)
  const problem = up.state === "FAILED" ? (t.dataProblem ?? t.err) : null
  return (
    <div className="flex min-h-0 flex-1 overflow-auto">
      {/* m-auto centres it both ways, and lets it scroll from its top when
          the pane is the shorter. Columns follow the pane's width. */}
      <div className="@container m-auto w-full max-w-5xl px-6 py-6">
        <div className="grid items-center gap-6 @2xl:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] @2xl:gap-10">
          <div className="flex min-w-0 flex-col gap-4">
            <div className="flex min-w-0 items-center gap-3">
              <div className="flex size-14 shrink-0 items-center justify-center rounded-2xl bg-muted text-muted-foreground">
                <FileTypeIcon contentType="" filename={up.name} size={26} />
              </div>
              <div className="flex min-w-0 flex-col gap-0.5">
                <h3 className="font-mono text-base font-medium break-all">{up.name}</h3>
                <p className="text-xs text-muted-foreground tabular-nums">
                  {up.sized ? formatBytes(up.size) : "size not known yet"}
                  {typeName && <> · {typeName}</>}
                  {up.stream && <> · streamed</>}
                </p>
              </div>
            </div>
            {dest && (
              <p className="flex min-w-0 items-baseline gap-1.5 text-xs text-muted-foreground">
                <ArrowRightIcon className="size-3.5 shrink-0 self-center" />
                <span className="font-mono break-all text-foreground">{dest}</span>
              </p>
            )}
            <div className="flex w-full max-w-sm flex-col gap-2">
              <div className={cn("inline-flex flex-wrap items-center gap-x-1.5 text-sm", state.className)}>
                <Icon className={cn("size-4", going && up.state !== "WAITING" && "animate-spin")} />
                {state.label}
                {up.state === "SENDING" && (
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {formatBytes(up.acked)} of {formatBytes(up.size)} ({pct}%)
                    {bps !== null && (
                      <>
                        {" "}· {formatBytes(bps)}/s · {duration((up.size - up.acked) / bps)} left
                      </>
                    )}
                  </span>
                )}
              </div>
              {going && (
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
                  <div className="h-full bg-sky-500 transition-[width]" style={{ width: `${pct}%` }} />
                </div>
              )}
              {problem && <p className="text-xs text-red-600 dark:text-red-400">{problem}</p>}
            </div>
            {up.stream && <StreamUploader task={t} upload={up} />}
          </div>

          <InfoCard labelWidth="w-24">
            <LabelRow label="File">
              <span className="font-mono text-xs break-all">{up.name}</span>
            </LabelRow>
            <LabelRow label="Size">
              {up.sized ? (
                <span className="tabular-nums">{up.size.toLocaleString("en-US")} bytes</span>
              ) : (
                <span className="text-muted-foreground">said by its uploader</span>
              )}
            </LabelRow>
            <LabelRow label="SHA-256">
              {!up.sha256 ? (
                <span className="text-muted-foreground">once core has all of it</span>
              ) : (
                <span className="flex min-w-0 items-center gap-1">
                  <code className="min-w-0 flex-1 truncate font-mono text-xs" title={up.sha256}>
                    {up.sha256}
                  </code>
                  <Button variant="ghost" size="icon-xs" className="-my-1" title="Copy the SHA-256" aria-label="Copy the SHA-256"
                    onClick={() => void copyToClipboard(up.sha256, "SHA-256")}>
                    <CopyIcon />
                  </Button>
                </span>
              )}
            </LabelRow>
            <LabelRow label="To">
              {dest ? <span className="font-mono text-xs break-all">{dest}</span> : <span className="text-muted-foreground">the minion says</span>}
            </LabelRow>
            <LabelRow label="State">
              <span className={state.className}>{state.label}</span>
              {up.state === "SENDING" && <span className="text-muted-foreground tabular-nums"> · {pct}%</span>}
            </LabelRow>
            <LabelRow label="Started">
              {up.firstAckAt ? (
                <span className="tabular-nums">{formatDateTime(up.firstAckAt)}</span>
              ) : (
                <span className="text-muted-foreground">at the minion's first ask</span>
              )}
            </LabelRow>
            {t.doneAt && (
              <LabelRow label="Ended">
                <span className="tabular-nums">{formatDateTime(t.doneAt)}</span>
              </LabelRow>
            )}
          </InfoCard>
        </div>
      </div>
    </div>
  )
}

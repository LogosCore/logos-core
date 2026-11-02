import { CheckIcon, CopyIcon, LoaderIcon, XIcon } from "lucide-react"
import { fileTypeName } from "@/components/file-preview/type-name"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import {
  FileOverviewBody,
  FileOverviewCards,
  FileProgress,
  FileStrip,
  ShaValue,
  SizeValue,
} from "./file-overview-parts"
import { rateText } from "./file-overview-values"
import { StreamUploader } from "./stream-uploader"
import { VerifyHashCard } from "./verify-hash-card"

// A file_upload task's file going to its minion (docs:
// contracts/task-results-and-actions.md, file_upload), and an upload_stream
// task's, laid out as a kept file's Overview (file-view.tsx): the file's
// name in the strip above; then, once written, what can be done with it,
// or how far the minion has it (its asks for chunks: the bar, the rate, the
// time left) and why it stopped; then its details and, once written, the
// commands that check it on the minion's host. Live: it follows the task's
// updates, one with each chunk the minion asks for.

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
  const done = up.state === "DONE"
  const going = up.state === "WAITING" || up.state === "SENDING" || up.state === "SENT"
  const pct = !up.sized ? 0 : up.size > 0 ? Math.min(100, Math.floor((up.acked / up.size) * 100)) : 100
  const Icon = done ? CheckIcon : up.state === "FAILED" || up.state === "CANCELLED" ? XIcon : LoaderIcon
  const dest = destination(t, data)
  const typeName = fileTypeName("", up.name)
  const problem = up.state === "FAILED" ? (t.dataProblem ?? t.err) : null
  const sha256 = up.sha256 || null

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <FileStrip name={up.name} size={up.sized ? up.size : null} />
      <FileOverviewBody>
        {done ? (
          <div className="flex flex-wrap items-center gap-2">
            {sha256 && (
              <Button variant="outline" onClick={() => void copyToClipboard(sha256, "SHA-256")}>
                <CopyIcon />
                Copy SHA-256
              </Button>
            )}
            {dest && (
              <Button variant="outline" onClick={() => void copyToClipboard(dest, "path")}>
                <CopyIcon />
                Copy path
              </Button>
            )}
          </div>
        ) : (
          <FileProgress
            label={state.label}
            className={state.className}
            icon={Icon}
            spin={going && up.state !== "WAITING" && !uploaderOff}
            detail={
              up.state === "SENDING" && up.sized
                ? `${formatBytes(up.acked)} of ${formatBytes(up.size)} (${pct}%)${rateText(rate(up), up.size - up.acked)}`
                : null
            }
            pct={going ? pct : null}
            barClassName={uploaderOff && up.uploaderState === "PAUSED" ? "bg-amber-500" : "bg-sky-500"}
            problem={problem}
          >
            {up.stream && <StreamUploader task={t} upload={up} />}
          </FileProgress>
        )}

        <FileOverviewCards>
          <InfoCard labelWidth="w-24">
            <LabelRow label="File">
              <span className="font-mono text-xs break-all">{up.name}</span>
              {up.stream && <span className="text-muted-foreground"> · streamed</span>}
            </LabelRow>
            <LabelRow label="To">
              {dest ? <span className="font-mono text-xs break-all">{dest}</span> : <span className="text-muted-foreground">the minion says</span>}
            </LabelRow>
            <LabelRow label="Size">
              {up.sized ? <SizeValue size={up.size} /> : <span className="text-muted-foreground">said by its uploader</span>}
            </LabelRow>
            <LabelRow label="Type">
              {typeName ? <span>{typeName}</span> : <span className="text-muted-foreground">by its name, not known</span>}
            </LabelRow>
            <LabelRow label="SHA-256">
              <ShaValue sha256={sha256} pending="once core has all of it" />
            </LabelRow>
            <LabelRow label="State">
              <span className={state.className}>{state.label}</span>
              {up.state === "SENDING" && up.sized && <span className="text-muted-foreground tabular-nums"> · {pct}%</span>}
              {t.doneAt && <span className="text-muted-foreground tabular-nums"> · {formatDateTime(t.doneAt)}</span>}
            </LabelRow>
            <LabelRow label="Started">
              {up.firstAckAt ? (
                <span className="tabular-nums">{formatDateTime(up.firstAckAt)}</span>
              ) : (
                <span className="text-muted-foreground">at the minion's first ask</span>
              )}
            </LabelRow>
          </InfoCard>
          {done && sha256 && dest && (
            <VerifyHashCard name={dest} sha256={sha256} title="Verify on the host" note="run on the minion's host" />
          )}
        </FileOverviewCards>
      </FileOverviewBody>
    </div>
  )
}

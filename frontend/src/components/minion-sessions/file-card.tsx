import { CheckIcon, CopyIcon, DownloadIcon, FileIcon, LoaderIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatBytes } from "@/lib/format-bytes"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"

// A file_download task's file (docs: contracts/task-results-and-actions.md,
// file_download): its name on the host, how much of it came, whether it
// checked out, and its bytes to download once kept. Live: it follows the
// task's updates as the chunks come.

const API_URL = import.meta.env.VITE_API_URL as string

type File = NonNullable<MinionTaskFieldsFragment["file"]>

const STATE: Record<string, { label: string; className: string }> = {
  RECEIVING: { label: "Coming", className: "text-sky-600 dark:text-sky-400" },
  JOINING: { label: "Checking", className: "text-sky-600 dark:text-sky-400" },
  COMPLETE: { label: "Kept", className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { label: "Failed", className: "text-red-600 dark:text-red-400" },
  CANCELLED: { label: "Cancelled", className: "text-muted-foreground" },
}

export function FileCard({ file: f }: { file: File }) {
  const state = STATE[f.state] ?? { label: f.state.toLowerCase(), className: "text-muted-foreground" }
  const coming = f.state === "RECEIVING" || f.state === "JOINING"
  const pct = f.size > 0 ? Math.min(100, Math.round((f.received / f.size) * 100)) : 100
  const Icon = f.state === "COMPLETE" ? CheckIcon : f.state === "FAILED" || f.state === "CANCELLED" ? XIcon : LoaderIcon
  return (
    <div className="m-3 flex max-w-2xl flex-col gap-3 rounded-lg border p-4">
      <div className="flex items-start gap-3">
        <FileIcon className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="font-mono text-sm break-all">{f.name}</span>
          <span className="text-xs text-muted-foreground tabular-nums">
            {formatBytes(f.size)}
            {f.doneAt && <> · {formatDateTime(f.doneAt)}</>}
          </span>
        </div>
        {f.state === "COMPLETE" && (
          <Button size="sm" render={<a href={`${API_URL}/minion-files/${encodeURIComponent(f.id)}`} download />}>
            <DownloadIcon />
            Download
          </Button>
        )}
      </div>

      <div className="flex flex-col gap-1">
        <div className={cn("inline-flex items-center gap-1.5 text-xs", state.className)}>
          <Icon className={cn("size-3.5", coming && "animate-spin")} />
          {state.label}
          {coming && (
            <span className="text-muted-foreground tabular-nums">
              · {formatBytes(f.received)} of {formatBytes(f.size)} ({pct}%)
            </span>
          )}
        </div>
        {coming && (
          <div className="h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full bg-sky-500 transition-[width]" style={{ width: `${pct}%` }} />
          </div>
        )}
        {f.err && <p className="text-xs text-red-600 dark:text-red-400">{f.err}</p>}
      </div>

      {f.sha256 && (
        <div className="flex items-center gap-2 text-xs">
          <span className="shrink-0 text-muted-foreground">SHA-256</span>
          <code className="min-w-0 flex-1 truncate font-mono" title={f.sha256}>
            {f.sha256}
          </code>
          <Button variant="ghost" size="icon-xs" title="Copy the SHA-256" aria-label="Copy the SHA-256" onClick={() => void copyToClipboard(f.sha256!, "SHA-256")}>
            <CopyIcon />
          </Button>
        </div>
      )}
    </div>
  )
}

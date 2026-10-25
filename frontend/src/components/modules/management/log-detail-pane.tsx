import { useState } from "react"
import { BracesIcon, CopyIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"
import type { LogLine } from "./log-buffer"
import { detailSections, recordJSON, type DetailRow } from "./log-detail"

const LEVEL_BADGE: Record<LogLine["level"], string> = {
  DEBUG: "bg-muted text-muted-foreground",
  INFO: "bg-sky-500/15 text-sky-700 dark:text-sky-400",
  WARN: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  ERROR: "bg-destructive/15 text-destructive",
}

interface LogDetailPaneProps {
  line: LogLine
  onClose: () => void
  className?: string
}

/**
 * One log record laid out for reading: header, then the record's attrs as the
 * module structured them (see log-detail.ts), or the raw JSON.
 */
export function LogDetailPane({ line, onClose, className }: LogDetailPaneProps) {
  const [raw, setRaw] = useState(false)
  const sections = detailSections(line.attrs)
  const local = new Date(line.ms)

  return (
    <aside
      className={cn("flex min-h-0 flex-col overflow-hidden rounded-xl border bg-card", className)}
      aria-label="Log line details"
    >
      <header className="flex items-start gap-2 border-b px-4 py-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <span className={cn("rounded px-1.5 py-0.5 font-mono text-[11px] font-semibold", LEVEL_BADGE[line.level])}>
              {line.level}
            </span>
            <time className="text-xs text-muted-foreground tabular-nums" dateTime={line.time} title={line.time}>
              {local.toLocaleDateString()} {local.toLocaleTimeString()}.{String(local.getMilliseconds()).padStart(3, "0")}
            </time>
          </div>
          <p className="text-sm font-medium break-words">{line.message}</p>
        </div>
        <Button
          variant="ghost"
          size="icon-sm"
          title="Copy as JSON"
          onClick={() => copyToClipboard(recordJSON(line), "log line")}
        >
          <CopyIcon />
        </Button>
        <Button variant="ghost" size="icon-sm" title="Close (Esc)" onClick={onClose}>
          <XIcon />
        </Button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {sections.length === 0 && !raw && (
          <p className="px-4 py-6 text-center text-sm text-muted-foreground">No fields on this line.</p>
        )}
        {!raw &&
          sections.map((s) => (
            <section key={s.path} className="border-b last:border-b-0">
              {s.path && (
                <h4 className="bg-muted/40 px-4 py-1.5 font-mono text-[11px] font-semibold text-muted-foreground">
                  {s.path}
                  <span className="ml-1 font-normal">· {s.rows.length}</span>
                </h4>
              )}
              <dl className="divide-y">
                {s.rows.map((r) => (
                  <FieldRow key={r.path} row={r} />
                ))}
              </dl>
            </section>
          ))}
        {raw && (
          <pre className="px-4 py-3 font-mono text-xs break-all whitespace-pre-wrap">{recordJSON(line)}</pre>
        )}
      </div>

      <footer className="flex items-center justify-between border-t px-4 py-2">
        <Button variant="ghost" size="sm" onClick={() => setRaw((r) => !r)}>
          <BracesIcon />
          {raw ? "Show fields" : "Raw JSON"}
        </Button>
        <span className="text-xs text-muted-foreground">↑ ↓ to move · Esc to close</span>
      </footer>
    </aside>
  )
}

function FieldRow({ row }: { row: DetailRow }) {
  return (
    <div className="group grid grid-cols-[minmax(7rem,38%)_1fr_auto] items-start gap-2 px-4 py-1.5 hover:bg-muted/40">
      <dt className="truncate font-mono text-xs text-muted-foreground" title={row.path}>
        {row.label}
      </dt>
      <dd className="min-w-0 font-mono text-xs break-all whitespace-pre-wrap">
        {row.text === "" ? <span className="text-muted-foreground">""</span> : row.text}
      </dd>
      <button
        type="button"
        className="opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        title="Copy value"
        onClick={() => copyToClipboard(row.text, row.label)}
      >
        <CopyIcon className="size-3.5 text-muted-foreground hover:text-foreground" />
      </button>
    </div>
  )
}

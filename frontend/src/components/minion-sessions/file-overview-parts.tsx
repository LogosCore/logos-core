import type { ReactNode } from "react"
import type { LucideIcon } from "lucide-react"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"

// The parts a file task's view is made of, as a file_download's Overview
// has them (file-view.tsx): the strip with the file's name above, the
// overview below from the pane's top left (what can be done with the file,
// or how far it has got and why it stopped), and its cards side by side
// where the pane is wide. An upload's and a stream's views use them so the
// three read alike.

/** The strip above a file's view: its type's icon, name and size; tools at its end. */
export function FileStrip({ name, size, children }: { name: string; size: number | null; children?: ReactNode }) {
  return (
    <div className="flex min-h-8 flex-wrap items-center gap-x-2 gap-y-1 border-b px-2 py-1">
      <span className="flex min-w-0 flex-1 items-baseline gap-1.5 px-1" title={name}>
        <FileTypeIcon contentType="" filename={name} className="size-3.5 shrink-0 self-center text-muted-foreground" />
        <span className="truncate font-mono text-xs font-medium">{name}</span>
        {size !== null && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatBytes(size)}</span>}
      </span>
      {children && <div className="ml-auto flex items-center gap-1">{children}</div>}
    </div>
  )
}

/** The overview's body, under the strip: top left, as wide as it reads well, scrolling in the pane. */
export function FileOverviewBody({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-0 flex-1 items-start overflow-auto">
      {/* Columns follow the pane's width, not the window's. */}
      <div className="@container flex w-full max-w-6xl flex-col gap-4 p-4">{children}</div>
    </div>
  )
}

/** The cards: one column, two where the pane is wide. */
export function FileOverviewCards({ children }: { children: ReactNode }) {
  return <div className="grid items-start gap-4 @3xl:grid-cols-2">{children}</div>
}

/**
 * Where a file stands while it moves, or why it stopped: its state with an
 * icon, how far (detail), a bar (pct, while it moves), and a problem.
 */
export function FileProgress({
  label,
  className,
  icon: Icon,
  spin,
  detail,
  pct,
  barClassName = "bg-sky-500",
  problem,
  children,
}: {
  label: string
  className: string
  icon: LucideIcon
  spin?: boolean
  detail?: ReactNode
  /** The bar's fill, 0 to 100; no bar without it. */
  pct?: number | null
  barClassName?: string
  problem?: string | null
  /** Under the bar: what goes with it (a stream's uploader). */
  children?: ReactNode
}) {
  return (
    <div className="flex w-full max-w-md flex-col gap-2">
      <div className={cn("inline-flex flex-wrap items-center gap-x-1.5 text-sm", className)}>
        <Icon className={cn("size-4", spin && "animate-spin")} />
        {label}
        {detail && <span className="text-xs text-muted-foreground tabular-nums">{detail}</span>}
      </div>
      {pct != null && (
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <div className={cn("h-full transition-[width]", barClassName)} style={{ width: `${pct}%` }} />
        </div>
      )}
      {problem && <p className="text-xs text-red-600 dark:text-red-400">{problem}</p>}
      {children}
    </div>
  )
}

/** A size in bytes, and readably past a KiB. */
export function SizeValue({ size }: { size: number }) {
  return (
    <>
      <span className="tabular-nums">{size.toLocaleString("en-US")} bytes</span>
      {size >= 1024 && <span className="text-muted-foreground tabular-nums"> · {formatBytes(size)}</span>}
    </>
  )
}

/** A SHA-256, whole and wrapped, or why it is not known yet. */
export function ShaValue({ sha256, pending }: { sha256: string | null | undefined; pending: string }) {
  return sha256 ? <code className="font-mono text-xs break-all">{sha256}</code> : <span className="text-muted-foreground">{pending}</span>
}

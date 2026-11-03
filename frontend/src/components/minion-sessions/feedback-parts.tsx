import { ChevronRightIcon, CircleAlertIcon, TriangleAlertIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { MinionFeedbackFieldsFragment } from "@/graphql/gql/graphql"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { FEEDBACK_TONE, feedbackLevel, feedbackSummary, firstLine, type FeedbackLevel } from "./feedback-values"

// A minion's feedback as the session's views show it: a level's icon, the
// counts, and an entry.

export function FeedbackLevelIcon({ level, className }: { level: FeedbackLevel; className?: string }) {
  const Icon = level === "error" ? CircleAlertIcon : TriangleAlertIcon
  return <Icon className={cn("size-3.5 shrink-0", FEEDBACK_TONE[level], className)} aria-label={level === "error" ? "Error" : "Warning"} />
}

/** Errors and warnings (unread ones, unless titled otherwise), each with its icon; nothing when there are none. */
export function FeedbackCounts({
  errors,
  warnings,
  className,
  title = `Unread feedback: ${feedbackSummary(errors, warnings)}`,
}: {
  errors: number
  warnings: number
  className?: string
  title?: string
}) {
  if (errors + warnings === 0) return null
  return (
    <span className={cn("inline-flex items-center gap-2 tabular-nums", className)} title={title}>
      {errors > 0 && (
        <span className={cn("inline-flex items-center gap-0.5", FEEDBACK_TONE.error)}>
          <CircleAlertIcon className="size-3.5" aria-hidden />
          {errors}
        </span>
      )}
      {warnings > 0 && (
        <span className={cn("inline-flex items-center gap-0.5", FEEDBACK_TONE.warning)}>
          <TriangleAlertIcon className="size-3.5" aria-hidden />
          {warnings}
        </span>
      )}
    </span>
  )
}

// Longer than this, a one-line message is cut until opened.
const LONG = 100

/**
 * One entry, a line: its level, when, its code, the message, how many
 * times, the task it is about (showTask). A long message shows its first
 * line until opened, in place, by a click on the line or its chevron.
 * compact: the console's size.
 */
export function FeedbackEntry({
  entry: e,
  open,
  onToggle,
  compact = false,
  showTask = false,
}: {
  entry: MinionFeedbackFieldsFragment
  open: boolean
  onToggle: () => void
  compact?: boolean
  showTask?: boolean
}) {
  const level = feedbackLevel(e.level)
  const when = e.at ?? e.receivedAt
  const { line, more } = firstLine(e.msg)
  const long = more > 0 || line.length > LONG
  const shut = long && !open
  return (
    <div
      className={cn(
        "flex items-baseline gap-2 border-b",
        compact ? "px-3 py-1 text-xs" : "px-4 py-1.5 text-sm",
        long && "cursor-pointer hover:bg-muted/60",
        e.unread && "bg-muted/40",
      )}
      // Not when text was selected in it: that was to copy it.
      onClick={long ? () => window.getSelection()?.isCollapsed !== false && onToggle() : undefined}
    >
      <FeedbackLevelIcon level={level} className="self-center" />
      <span className="shrink-0 text-muted-foreground tabular-nums" title={e.at ? `Received ${formatDateTime(e.receivedAt)}` : undefined}>
        {formatDateTime(when)}
      </span>
      {e.code && <span className="shrink-0 font-mono text-xs text-muted-foreground">{e.code}</span>}
      <span className={cn("min-w-0 flex-1", shut ? "truncate" : "break-words whitespace-pre-wrap", e.unread && "font-medium")}>
        {shut ? line : e.msg}
      </span>
      {shut && more > 0 && (
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
          +{more} {more === 1 ? "line" : "lines"}
        </span>
      )}
      {e.count > 1 && (
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums" title={`Happened ${e.count} times`}>
          ×{e.count}
        </span>
      )}
      {showTask && e.taskId && (
        <span className="shrink-0 font-mono text-xs text-muted-foreground" title={`About task ${e.taskId}`}>
          task {e.taskId.slice(0, 8)}
        </span>
      )}
      {long && (
        <Button
          variant="ghost"
          size="icon-xs"
          className="self-start"
          aria-label={open ? "Show its first line" : "Show all of it"}
          aria-expanded={open}
          onClick={(ev) => {
            ev.stopPropagation()
            onToggle()
          }}
        >
          <ChevronRightIcon className={cn("transition-transform", open && "rotate-90")} />
        </Button>
      )}
    </div>
  )
}

import { cn } from "@/lib/utils"

// Health is the module's own heartbeat judgement, a platform value defined by
// the lifecycle contract (healthy | degraded), so core can colour it. Same
// dot-and-text idiom as ModuleStatusIndicator. A module that is not
// registered has no current health: its last report is shown greyed out.
const HEALTH_STYLES: Record<string, { dot: string; text: string }> = {
  healthy: {
    dot: "bg-green-600 dark:bg-green-400",
    text: "text-green-600 dark:text-green-400",
  },
  degraded: {
    dot: "bg-amber-500 dark:bg-amber-400",
    text: "text-amber-600 dark:text-amber-400",
  },
}

const STALE = { dot: "bg-muted-foreground/50", text: "text-muted-foreground" }

export function ModuleHealthIndicator({
  health,
  registered,
  className,
}: {
  health: string
  registered: boolean
  className?: string
}) {
  if (!health) return <span className={cn("text-sm text-muted-foreground", className)}>—</span>
  const s = (registered && HEALTH_STYLES[health]) || STALE
  return (
    <span
      className={cn("inline-flex items-center gap-1.5 text-sm", s.text, className)}
      title={registered ? undefined : "Last reported before the module went away"}
    >
      <span className={cn("size-2 rounded-full", s.dot)} />
      {health}
    </span>
  )
}

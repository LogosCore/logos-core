import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

// One look for every module state (registration, health, config sync): a
// coloured dot and a lowercase label. Tones mean the same everywhere.
export type StateTone = "ok" | "warn" | "bad" | "idle"

const TONES: Record<StateTone, { dot: string; text: string }> = {
  ok: { dot: "bg-green-600 dark:bg-green-400", text: "text-green-700 dark:text-green-400" },
  warn: { dot: "bg-amber-500 dark:bg-amber-400", text: "text-amber-700 dark:text-amber-400" },
  bad: { dot: "bg-red-600 dark:bg-red-400", text: "text-red-700 dark:text-red-400" },
  idle: { dot: "bg-muted-foreground/50", text: "text-muted-foreground" },
}

export function StateIndicator({
  tone,
  children,
  title,
  pulse,
  className,
}: {
  tone: StateTone
  children: ReactNode
  title?: string
  // A transitional state (e.g. a config being applied).
  pulse?: boolean
  className?: string
}) {
  const t = TONES[tone]
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-sm", t.text, className)} title={title}>
      <span className={cn("size-2 shrink-0 rounded-full", t.dot, pulse && "animate-pulse")} aria-hidden />
      {children}
    </span>
  )
}

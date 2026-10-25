import type { ReactNode } from "react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
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

/** What a state means, shown on hover. */
export interface StateHint {
  title: string
  body: ReactNode
  // Current facts (times, revisions, errors), set apart below the body.
  detail?: ReactNode
}

export function StateIndicator({
  tone,
  children,
  hint,
  pulse,
  className,
}: {
  tone: StateTone
  children: ReactNode
  hint?: StateHint
  // A transitional state (e.g. a config being applied).
  pulse?: boolean
  className?: string
}) {
  const t = TONES[tone]
  const indicator = (
    <span className={cn("inline-flex items-center gap-1.5 text-sm", t.text, hint && "cursor-help", className)}>
      <span className={cn("size-2 shrink-0 rounded-full", t.dot, pulse && "animate-pulse")} aria-hidden />
      {children}
    </span>
  )
  if (!hint) return indicator
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" tabIndex={0} />}>{indicator}</TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <div className="flex flex-col gap-1 text-left">
          <span className="font-semibold">{hint.title}</span>
          <span>{hint.body}</span>
          {hint.detail && <span className="opacity-75">{hint.detail}</span>}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

import { Fragment, type ComponentType } from "react"
import {
  CircleCheckIcon,
  CircleDashedIcon,
  CircleMinusIcon,
  CircleSlashIcon,
  CircleXIcon,
  LoaderCircleIcon,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { useSteadyTitle } from "@/hooks/use-steady-title"
import { formatElapsed } from "@/components/modules/management/field-values"
import { stageDuration, type StageLike } from "./build-values"

const STAGE: Record<string, { icon: ComponentType<{ className?: string }>; className: string; label: string }> = {
  PENDING: { icon: CircleDashedIcon, className: "text-muted-foreground", label: "Waiting" },
  RUNNING: { icon: LoaderCircleIcon, className: "animate-spin text-sky-600 dark:text-sky-400", label: "Running" },
  SUCCEEDED: { icon: CircleCheckIcon, className: "text-emerald-600 dark:text-emerald-400", label: "Done" },
  FAILED: { icon: CircleXIcon, className: "text-destructive", label: "Failed" },
  CANCELLED: { icon: CircleSlashIcon, className: "text-muted-foreground", label: "Cancelled" },
  SKIPPED: { icon: CircleMinusIcon, className: "text-muted-foreground/60", label: "Not run" },
}

const stageOf = (state: string) => STAGE[state] ?? STAGE.PENDING

/** A stage's state as an icon, coloured and spinning like its state. */
export function StageIcon({ state, className }: { state: string; className?: string }) {
  const { icon: Icon, className: tone } = stageOf(state)
  return <Icon aria-hidden className={cn("size-4 shrink-0", tone, className)} />
}

/** A stage's line for a tooltip: "Compile: Running · 12s · compiling 3 of 10". */
function stageTitle(s: StageLike, now: number): string {
  const d = stageDuration(s, now)
  return [`${s.label}: ${stageOf(s.state).label}`, d === null ? null : formatElapsed(d), s.message]
    .filter(Boolean)
    .join(" · ")
}

// Each stage's hover holds still while the pointer is on it (useSteadyTitle):
// a running stage's time moves on every tick, and its message as it goes.

/**
 * A build's pipeline in one row of icons, for a table; each says its stage on
 * hover. now times the running stage (useNow). buildId names the build, for a
 * table that keys its rows by place.
 */
export function PipelineDots({ stages, now, buildId }: { stages: readonly StageLike[]; now: number; buildId?: string }) {
  return (
    <span className="flex min-w-0 items-center overflow-hidden" role="list" aria-label="Pipeline">
      {stages.map((s, i) => (
        <Fragment key={`${s.id}-${i}`}>
          {i > 0 && <span aria-hidden className="h-px w-1.5 shrink-0 bg-border" />}
          <StageDot stage={s} now={now} buildId={buildId} />
        </Fragment>
      ))}
    </span>
  )
}

function StageDot({ stage: s, now, buildId }: { stage: StageLike; now: number; buildId?: string }) {
  const title = stageTitle(s, now)
  const hover = useSteadyTitle(title, buildId)
  return (
    <span role="listitem" aria-label={title} className="flex" {...hover}>
      <StageIcon state={s.state} className="size-3.5" />
    </span>
  )
}

/**
 * A build's pipeline: core's queue, the factory's steps, core's artifact
 * check, each with its state and how long it took, and the factory's message
 * on the step it is on. In a row joined by lines where there is room, one
 * stage per line on a narrow screen. now times the running stage (useNow).
 */
export function BuildPipeline({ stages, now }: { stages: readonly StageLike[]; now: number }) {
  return (
    <ol className="flex flex-col gap-2 sm:flex-row sm:items-start sm:gap-0 sm:overflow-x-auto sm:pb-1" aria-label="Pipeline">
      {stages.map((s, i) => (
        <li key={`${s.id}-${i}`} className="flex min-w-0 items-start sm:shrink-0">
          {i > 0 && <span aria-hidden className="mt-4 hidden h-px w-6 shrink-0 bg-border sm:block" />}
          <Stage stage={s} now={now} />
        </li>
      ))}
    </ol>
  )
}

function Stage({ stage: s, now }: { stage: StageLike; now: number }) {
  const d = stageDuration(s, now)
  const live = s.state === "RUNNING" || s.state === "FAILED" || s.state === "CANCELLED"
  const hover = useSteadyTitle(stageTitle(s, now))
  return (
    <div className="flex min-w-0 flex-col gap-1 sm:max-w-56">
      <div
        className={cn(
          "flex min-w-0 items-center gap-2 rounded-full border bg-card px-3 py-1.5 text-sm",
          s.state === "RUNNING" && "border-sky-600/40",
          s.state === "FAILED" && "border-destructive/40",
          (s.state === "PENDING" || s.state === "SKIPPED") && "border-dashed",
        )}
        {...hover}
      >
        <StageIcon state={s.state} />
        <span className={cn("truncate", s.core && "text-muted-foreground", s.state === "SKIPPED" && "text-muted-foreground/70")}>
          {s.label}
        </span>
        {d !== null && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatElapsed(d)}</span>}
      </div>
      {live && s.message && <p className="px-3 text-xs break-words text-muted-foreground sm:max-w-56">{s.message}</p>}
    </div>
  )
}

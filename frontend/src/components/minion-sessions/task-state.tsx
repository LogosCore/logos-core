import { BanIcon, CheckIcon, CircleDashedIcon, ClockIcon, FactoryIcon, LoaderIcon, SendIcon, StarIcon, WorkflowIcon, XIcon } from "lucide-react"
import { useSteadyTitle } from "@/hooks/use-steady-title"
import { formatDateTime } from "@/lib/date-format"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { clock, FINISHED, took, type Task } from "./task-actions"
import { compositeStands, phaseWord } from "./composite-values"
import { factoryHeld, factoryStands, ranOn } from "./factory-values"

// Where a task stands, as the console's task list and the task it shows
// draw it.

const STATE: Record<string, { icon: typeof CheckIcon; className: string; spin?: boolean }> = {
  // Queued with a time that has not come: nothing of it went anywhere.
  SCHEDULED: { icon: ClockIcon, className: "text-muted-foreground" },
  QUEUED: { icon: CircleDashedIcon, className: "text-muted-foreground" },
  SENT: { icon: SendIcon, className: "text-muted-foreground" },
  RUNNING: { icon: LoaderIcon, className: "text-sky-600 dark:text-sky-400", spin: true },
  // A composite command's task while its factory's code decides, in core.
  COMPOSING: { icon: WorkflowIcon, className: "text-violet-600 dark:text-violet-400" },
  // A factory command's task, which its factory runs itself, until it ends.
  FACTORY: { icon: FactoryIcon, className: "text-violet-400 dark:text-violet-300" },
  DONE: { icon: CheckIcon, className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { icon: XIcon, className: "text-red-600 dark:text-red-400" },
  CANCELLED: { icon: BanIcon, className: "text-muted-foreground" },
}

/** Waiting tasks that cannot reach their minion now (useTaskWaiting): amber. */
const HELD = "text-amber-600 dark:text-amber-400"

/** Whether a task in state waits on its minion being reached. */
function waits(state: string): boolean {
  return state === "QUEUED" || state === "SENT"
}

/** How a task's state is drawn: a command that exited non-zero as failed. */
function stateStyle(t: Task) {
  return t.state === "DONE" && t.exit != null && t.exit !== 0 ? STATE.FAILED : (STATE[t.state] ?? STATE.QUEUED)
}

/**
 * Why a waiting task is held, for its amber and its hover: its minion
 * cannot be reached (held), or no instance of its factory took a factory
 * command's task (core's reason). Null for one that is not.
 */
function heldWhy(t: Task, held?: string | null): string | null {
  return held && waits(t.state) ? held : factoryHeld(t)
}

/**
 * A task's state as an icon: a list row's. A command that exited non-zero
 * is marked as failed; a waiting task that cannot reach its minion (held,
 * the reason), or a factory command's task no instance took, in amber, the
 * reason on hover; one its factory runs pulses.
 */
export function StateIcon({ t, className, held }: { t: Task; className?: string; held?: string | null }) {
  const s = stateStyle(t)
  const Icon = s.icon
  const why = heldWhy(t, held)
  // The reason counts the minutes the target has been out: held while
  // hovered (useSteadyTitle).
  const hover = useSteadyTitle(why ? `Waiting: ${why}` : undefined)
  return (
    <Icon
      aria-label={t.state.toLowerCase()}
      className={cn(
        "size-3.5 shrink-0",
        why ? HELD : s.className,
        s.spin && "animate-spin",
        factoryStands(t)?.phase === "running" && "animate-pulse",
        className,
      )}
      onPointerEnter={hover.onPointerEnter}
      onPointerLeave={hover.onPointerLeave}
    >
      {hover.title && <title>{hover.title}</title>}
    </Icon>
  )
}

/** A marked task's star where it went well: the classic yellow. */
export const STAR = "text-yellow-500 dark:text-yellow-400"

/**
 * A task marked important, as a list row shows it in place of its state: a
 * star, outlined as the state icons it stands in for are, yellow when it
 * went well, else in its state's color (red failed, blue running and
 * pulsing, amber held, grey waiting or stopped), the reason on hover for a
 * held one.
 */
export function ImportantIcon({ t, className, held }: { t: Task; className?: string; held?: string | null }) {
  const s = stateStyle(t)
  const why = heldWhy(t, held)
  const hover = useSteadyTitle(why ? `Waiting: ${why}` : undefined)
  return (
    <StarIcon
      aria-label={`important, ${t.state.toLowerCase()}`}
      className={cn(
        "size-3.5 shrink-0",
        why ? HELD : s === STATE.DONE ? STAR : s.className,
        (s.spin || factoryStands(t)?.phase === "running") && "animate-pulse",
        className,
      )}
      onPointerEnter={hover.onPointerEnter}
      onPointerLeave={hover.onPointerLeave}
    >
      {hover.title && <title>{hover.title}</title>}
    </StarIcon>
  )
}

/**
 * A task's state in words, with its exit code and how long it ran; for a
 * waiting one that cannot reach its minion (held), why.
 */
export function StateLabel({ t, held }: { t: Task; held?: string | null }) {
  // A composite task's stop shows in its phase (compositeStands while it
  // composes, phaseWord while its own op is out); a cancel of its own op is
  // core's.
  const stopping = !t.composite && t.cancelRequestedAt && !FINISHED.has(t.state)
  const phase = phaseWord(t)
  const muted = "text-muted-foreground"
  let text: React.ReactNode
  switch (t.state) {
    case "SCHEDULED":
      text = (
        <span title="Core keeps it until then; then it goes on as one queued at that time (Run now: at once)">
          scheduled for {t.scheduledAt ? formatDateTime(t.scheduledAt) : "later"}
        </span>
      )
      break
    case "QUEUED":
      text = held ? (
        <span className={HELD}>waits: {held}</span>
      ) : (
        <span title="It goes out when the minion next checks in and asks for tasks">waits for the next check-in</span>
      )
      break
    case "SENT":
      text = (
        <span>
          sent{t.sendCount > 1 ? ` ×${t.sendCount}` : ""}, not acked yet
          {held && <span className={HELD}> · {held}</span>}
        </span>
      )
      break
    case "RUNNING":
      text = <span>{t.composite ? "its own op runs" : "running"}</span>
      break
    case "COMPOSING":
      text = <span>{compositeStands(t) ?? "composing"}</span>
      break
    case "FACTORY":
      text = <FactoryStandText t={t} />
      break
    case "DONE":
      text = <span>{t.exit != null ? `exit ${t.exit}` : "done"}</span>
      break
    default:
      text = <span>{t.state.toLowerCase()}</span>
  }
  const s = STATE[t.state]
  const failedExit = t.state === "DONE" && t.exit != null && t.exit !== 0
  const ran = ranOn(t)
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      <StateIcon t={t} />
      <span className={failedExit ? STATE.FAILED.className : s?.className}>{text}</span>
      {took(t) && <span className={muted}>· {took(t)}</span>}
      {ran && (
        <span className={muted} title="Its factory ran it, on this instance: nothing went to the minion">
          · ran on {ran}
        </span>
      )}
      {stopping && <span className="text-amber-600 dark:text-amber-400">· stopping{t.cancelReason && " (core)"}</span>}
      {phase && <span className="text-amber-600 dark:text-amber-400">· {phase}</span>}
      {t.composite?.silentSince && !FINISHED.has(t.state) && (
        <span className={HELD} title={`Core calls it again; 10 minutes without an answer fail the task. Since ${formatAbsolute(t.composite.silentSince)}`}>
          · its factory does not answer, since {clock(t.composite.silentSince)}
        </span>
      )}
      {!stopping && t.cancelReason && (
        <span className={muted} title={t.cancelReason}>
          · stopped by core
        </span>
      )}
    </span>
  )
}

/**
 * A factory command's task's words while it runs: why no instance took it
 * (core's reason, amber, as a held task's), or the instance core offered it
 * to, or that runs it.
 */
function FactoryStandText({ t }: { t: Task }) {
  const s = factoryStands(t)
  if (!s) return <span>its factory runs it</span>
  switch (s.phase) {
    case "waiting": {
      const since = t.factory?.waitingSince
      return s.why ? (
        <span className={HELD} title={`No instance of ${s.module} took it${since ? ` since ${formatAbsolute(since)}` : ""}: core offers it again every 2 seconds`}>
          waits: {s.why}
        </span>
      ) : (
        <span title="Core offers it to its factory's instances, the fewest-loaded first, every 2 seconds until one takes it">
          waits for an instance of {s.module}
        </span>
      )
    }
    case "offered":
      return <span title="Core's call is out: the instance takes it, or core offers it to the next">offered to {s.instance}</span>
    case "running": {
      const at = t.factory?.acceptedAt
      return <span title={`Its factory runs it: nothing goes to the minion${at ? `. Taken ${formatAbsolute(at)}` : ""}`}>{s.instance} runs it</span>
    }
  }
}

/** The times a task went through, short, each with its full time on hover. */
export function TaskTimes({ t }: { t: Task }) {
  const times = [
    ["queued", t.queuedAt],
    // The time it was queued with, kept once it went on.
    [t.state === "SCHEDULED" ? "scheduled for" : "scheduled", t.scheduledAt],
    ["sent", t.sentAt],
    ["started", t.startedAt],
    ["ended", t.doneAt],
  ].filter((p): p is [string, string] => !!p[1])
  return (
    <span className="inline-flex flex-wrap gap-x-2 text-xs text-muted-foreground">
      {times.map(([what, at]) => (
        <span key={what} title={formatAbsolute(at)}>
          {what} <span className="font-mono tabular-nums">{clock(at)}</span>
        </span>
      ))}
    </span>
  )
}

import { BanIcon, CheckIcon, CircleDashedIcon, LoaderIcon, SendIcon, XIcon } from "lucide-react"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { clock, FINISHED, took, type Task } from "./task-actions"

// Where a task stands, as the console's task list and the task it shows
// draw it.

const STATE: Record<string, { icon: typeof CheckIcon; className: string; spin?: boolean }> = {
  QUEUED: { icon: CircleDashedIcon, className: "text-muted-foreground" },
  SENT: { icon: SendIcon, className: "text-muted-foreground" },
  RUNNING: { icon: LoaderIcon, className: "text-sky-600 dark:text-sky-400", spin: true },
  DONE: { icon: CheckIcon, className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { icon: XIcon, className: "text-red-600 dark:text-red-400" },
  CANCELLED: { icon: BanIcon, className: "text-muted-foreground" },
}

/** A task's state as an icon: a list row's. A command that exited non-zero is marked as failed. */
export function StateIcon({ t, className }: { t: Task; className?: string }) {
  const s = t.state === "DONE" && t.exit != null && t.exit !== 0 ? STATE.FAILED : (STATE[t.state] ?? STATE.QUEUED)
  const Icon = s.icon
  return <Icon aria-label={t.state.toLowerCase()} className={cn("size-3.5 shrink-0", s.className, s.spin && "animate-spin", className)} />
}

/** A task's state in words, with its exit code and how long it ran. */
export function StateLabel({ t }: { t: Task }) {
  const stopping = t.cancelRequestedAt && !FINISHED.has(t.state)
  const muted = "text-muted-foreground"
  let text: React.ReactNode
  switch (t.state) {
    case "QUEUED":
      text = <span title="It goes out when the minion next checks in and asks for tasks">waits for the next check-in</span>
      break
    case "SENT":
      text = <span>sent{t.sendCount > 1 ? ` ×${t.sendCount}` : ""}, not acked yet</span>
      break
    case "RUNNING":
      text = <span>running</span>
      break
    case "DONE":
      text = <span>{t.exit != null ? `exit ${t.exit}` : "done"}</span>
      break
    default:
      text = <span>{t.state.toLowerCase()}</span>
  }
  const s = STATE[t.state]
  const failedExit = t.state === "DONE" && t.exit != null && t.exit !== 0
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      <StateIcon t={t} />
      <span className={failedExit ? STATE.FAILED.className : s?.className}>{text}</span>
      {took(t) && <span className={muted}>· {took(t)}</span>}
      {stopping && <span className="text-amber-600 dark:text-amber-400">· stopping</span>}
    </span>
  )
}

/** The times a task went through, short, each with its full time on hover. */
export function TaskTimes({ t }: { t: Task }) {
  const times = [
    ["queued", t.queuedAt],
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

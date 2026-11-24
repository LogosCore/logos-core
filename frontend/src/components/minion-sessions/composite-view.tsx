import { ListTreeIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import { formatAbsolute } from "@/lib/relative-time"
import { lineOf, type CommandDef } from "./command-line"
import { compositeStands, ownOpText } from "./composite-values"
import { FINISHED, type Task } from "./task-actions"

// A composite command's task, as its tab shows it (docs:
// contracts/task-outsourcing): what its factory's code decided so far. Its
// status line, where it stands, its own op (and the args a decision ran it
// with), its subtasks (shown in the list, the composite task with them), its
// timer, whether its factory answers, its stop, and the subtask whose result
// it took.

const HELD = "text-amber-600 dark:text-amber-400"

export function CompositeView({
  task: t,
  command,
  showingSubtasks,
  onShowComposite,
}: {
  task: Task
  command?: CommandDef
  /** Whether the list shows this task with its subtasks now. */
  showingSubtasks: boolean
  /** Shows the task with its subtasks in the list, pick picked there. */
  onShowComposite?: (id: string, pick?: string) => void
}) {
  const c = t.composite
  if (!c) return null
  const ended = FINISHED.has(t.state)
  const own = ownOpText(c)
  const subtasks = c.subtasksUnderWay + c.subtasksEnded
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      <InfoCard labelWidth="w-36">
        <LabelRow label="Status">
          {c.status ? <span className="whitespace-pre-wrap">{c.status}</span> : <span className="text-muted-foreground">none said</span>}
        </LabelRow>
        {!ended && <LabelRow label="Where it stands">{compositeStands(t) ?? t.state.toLowerCase()}</LabelRow>}
        {own !== null && (
          <LabelRow label="Its own op">
            <span>{own}</span>
            {c.ownArgs != null && (
              <span className="mt-0.5 block text-xs text-muted-foreground">
                ran with{" "}
                <code className="font-mono break-all text-foreground">
                  {lineOf(t.command, c.ownArgs as Record<string, unknown>, command)}
                </code>
              </span>
            )}
          </LabelRow>
        )}
        <LabelRow label="Subtasks">
          <span className="inline-flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="tabular-nums">
              {subtasks === 0 ? "none yet" : `${c.subtasksUnderWay} under way · ${c.subtasksEnded} ended`}
            </span>
            {subtasks > 0 && onShowComposite && !showingSubtasks && (
              <Button variant="outline" size="xs" className="self-center" onClick={() => onShowComposite(t.id, t.id)}>
                <ListTreeIcon />
                Show them in the list
              </Button>
            )}
          </span>
        </LabelRow>
        {t.resultOf && (
          <LabelRow label="Its result">
            <span className="inline-flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <span>a subtask&apos;s: its data, output and file</span>
              {onShowComposite && (
                <Button variant="outline" size="xs" className="self-center" onClick={() => onShowComposite(t.id, t.resultOf ?? undefined)}>
                  Show it
                </Button>
              )}
            </span>
          </LabelRow>
        )}
        {c.timerAt && !ended && <LabelRow label="Its timer">its factory is called again at {formatAbsolute(c.timerAt)}, unless something ends first</LabelRow>}
        <LabelRow label="Its factory">
          <span className="tabular-nums">
            {c.calls} {c.calls === 1 ? "call" : "calls"}
          </span>
          {c.silentSince && !ended && (
            <span className={`mt-0.5 block ${HELD}`}>
              does not answer since {formatAbsolute(c.silentSince)}: core calls it again, and 10 minutes without an answer fail the task
            </span>
          )}
        </LabelRow>
        {c.stoppedAt && <LabelRow label="Stopped">{formatAbsolute(c.stoppedAt)}</LabelRow>}
      </InfoCard>
    </div>
  )
}

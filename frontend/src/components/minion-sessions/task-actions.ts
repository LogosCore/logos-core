import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { fetchMinionTaskOutput, useCancelMinionTask, useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatInterval } from "./sessions-layout"
import { formatTime } from "@/lib/date-format"

// What the console's task list and the task it shows share: a task's
// times, and what can be done with it (task-state.tsx draws where it
// stands).

export type Task = MinionTaskFieldsFragment

export const FINISHED = new Set(["DONE", "FAILED", "CANCELLED"])

/** HH:MM:SS of a time. */
export function clock(at: string): string {
  return formatTime(at)
}

/** How long it ran, from when it went out to its end. */
export function took(t: Task): string | null {
  const from = t.startedAt ?? t.sentAt
  if (!from || !t.doneAt) return null
  return formatInterval(Math.max(0, Date.parse(t.doneAt) - Date.parse(from)))
}

/**
 * What can be done with a task: stop it, run it again (onQueued gets the new
 * one), copy its command line or output. Each says false when it does not
 * apply.
 */
export function useTaskActions(t: Task, line: string, { canWrite, onQueued }: { canWrite: boolean; onQueued?: (t: Task) => void }) {
  const cancel = useCancelMinionTask()
  const queue = useQueueMinionTask()
  const finished = FINISHED.has(t.state)
  return {
    canStop: canWrite && !finished && !t.cancelRequestedAt,
    stopping: cancel.isPending,
    // mutateAsync, not mutate's callbacks: a menu that ran it closes, and
    // callbacks do not run once the caller is gone.
    stop: () => void cancel.mutateAsync(t.id).catch((err: unknown) => toast.error(errorMessage(err, "Could not stop the task"))),
    canRunAgain: canWrite,
    runningAgain: queue.isPending,
    runAgain: () =>
      void queue
        .mutateAsync({ sessionId: t.sessionId, command: t.command, args: (t.args as Record<string, unknown> | null) ?? null })
        .then(
          (data) => onQueued?.(data.queueMinionTask),
          (err: unknown) => toast.error(errorMessage(err, "Could not queue the task")),
        ),
    copyCommand: () => void copyToClipboard(line, "command"),
    hasOutput: t.outputSize > 0,
    copyOutput: async (output?: string) => {
      try {
        await copyToClipboard(output ?? (await fetchMinionTaskOutput(t.id)), "output")
      } catch (err) {
        toast.error(errorMessage(err, "Could not read the output"))
      }
    },
  }
}

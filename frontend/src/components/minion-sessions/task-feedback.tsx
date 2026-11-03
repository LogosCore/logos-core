import { useState } from "react"
import { LoaderIcon } from "lucide-react"
import type { MinionFeedbackFieldsFragment } from "@/graphql/gql/graphql"
import { FeedbackEntry } from "./feedback-parts"
import { feedbackSummary, toggled } from "./feedback-values"

// A task's Feedback tab in the console: what its minion said of it (a
// crash, a test's words), newest first, one line each; a long one opens in
// place on a click. The session's Feedback has the rest and marks it read.

export function TaskFeedbackView({
  entries,
  errors,
  warnings,
  loading,
  onOpenSession,
}: {
  /** The latest of it, as core gives them with the task. */
  entries: readonly MinionFeedbackFieldsFragment[]
  /** How many came about it, all of them. */
  errors: number
  warnings: number
  loading: boolean
  /** Opens the session's Feedback: all of it, with Mark all read. */
  onOpenSession: () => void
}) {
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  const total = errors + warnings
  if (loading && entries.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center p-6 text-muted-foreground">
        <LoaderIcon className="size-4 animate-spin" />
      </div>
    )
  }
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      {entries.map((e) => (
        <FeedbackEntry key={e.id} entry={e} compact open={open.has(e.id)} onToggle={() => setOpen((o) => toggled(o, e.id))} />
      ))}
      <p className="flex flex-wrap items-baseline gap-x-1 px-3 py-1.5 text-xs text-muted-foreground">
        {entries.length === 0
          ? "None of it is kept any more: a session keeps its newest feedback."
          : entries.length < total
            ? `The latest ${entries.length} of ${total}.`
            : `${feedbackSummary(errors, warnings)}.`}
        <button type="button" className="cursor-pointer underline-offset-2 hover:text-foreground hover:underline" onClick={onOpenSession}>
          All of the session's feedback
        </button>
      </p>
    </div>
  )
}

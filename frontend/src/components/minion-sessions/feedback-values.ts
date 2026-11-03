// A minion's feedback (docs: contracts/minion-messages/sessions, Feedback):
// the warnings and errors it sends its operators, as the session's views say
// them.

export type FeedbackLevel = "warning" | "error"

/** A level as core gives it: one a later version adds is a warning. */
export function feedbackLevel(level: string): FeedbackLevel {
  return level === "error" ? "error" : "warning"
}

/** "3 errors, 1 warning"; "" for none. */
export function feedbackSummary(errors: number, warnings: number): string {
  const parts = [
    errors > 0 ? `${errors} ${errors === 1 ? "error" : "errors"}` : "",
    warnings > 0 ? `${warnings} ${warnings === 1 ? "warning" : "warnings"}` : "",
  ]
  return parts.filter(Boolean).join(", ")
}

/** The tone of a level: red for errors, amber for warnings. */
export const FEEDBACK_TONE: Record<FeedbackLevel, string> = {
  error: "text-red-600 dark:text-red-400",
  warning: "text-amber-700 dark:text-amber-400",
}

/** The ids of the entries shown whole, with id's turned. */
export function toggled(open: ReadonlySet<string>, id: string): ReadonlySet<string> {
  const next = new Set(open)
  if (!next.delete(id)) next.add(id)
  return next
}

/** A message's first line, and how many more it has. */
export function firstLine(msg: string): { line: string; more: number } {
  const lines = msg.trimEnd().split("\n")
  return { line: lines[0], more: lines.length - 1 }
}

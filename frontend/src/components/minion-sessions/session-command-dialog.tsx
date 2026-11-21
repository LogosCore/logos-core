import type { ReactNode } from "react"
import type { Draft } from "@/components/modules/management/field-values"
import { CommandFormDialog } from "@/components/command-form/command-form-dialog"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { startStreamUpload } from "@/lib/stream-upload"
import { cn } from "@/lib/utils"
import type { ConsoleCommand } from "./command-bar"
import { commandKindsNote } from "./command-kinds"
import { lineOf, usage } from "./command-line"
import type { ConnectionChoice } from "./connection-choices"

// A session's command as a form, wherever it opens: the console's
// Shift+Enter and form button, and the views that run one of the session's
// commands (a connection's Edit). One form, so a command looks and queues
// the same from each: its label, how it is typed, its description and
// notes, the fields, and the line it is the same as.

export interface SessionCommandDialogProps {
  sessionId: string
  /** The command; undefined while one is to be picked (before). */
  command: ConsoleCommand | undefined
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Where the fields start (what was typed, a connection picked); the defaults when absent. */
  initialDrafts?: Record<string, Draft>
  /** Fields whose problems show from the start. */
  showErrorsFor?: readonly string[]
  /** A new key starts the form over. */
  formKey: string
  /** The session's connections, for the line a connection param is named on. */
  connections: readonly ConnectionChoice[]
  /** The kind of connection the minion uses now, for a command that works over some kinds only. */
  connKind?: string | null
  /** Above the fields: the console's command picker. */
  before?: ReactNode
  /** The task queued. */
  onQueued?: (task: MinionTaskFieldsFragment) => void
}

export function SessionCommandDialog({
  sessionId,
  command,
  open,
  onOpenChange,
  initialDrafts,
  showErrorsFor,
  formKey,
  connections,
  connKind,
  before,
  onQueued,
}: SessionCommandDialogProps) {
  const queue = useQueueMinionTask()
  return (
    <CommandFormDialog
      open={open}
      onOpenChange={onOpenChange}
      formKey={formKey}
      title={command ? command.label : "Run a command"}
      description={
        command ? (
          <>
            <code className="font-mono text-xs">{usage(command)}</code>
            {command.description && <span className="mt-1 block">{command.description}</span>}
            {command.composite && <CompositeLine uses={command.uses} />}
            <KindsLine kinds={command.kinds} connKind={connKind} />
          </>
        ) : (
          "Pick one of the session's commands."
        )
      }
      before={before}
      fields={command?.fields ?? []}
      initialDrafts={initialDrafts}
      showErrorsFor={showErrorsFor}
      after={(values) =>
        command && (
          <p
            className="truncate font-mono text-xs text-muted-foreground"
            title={values ? lineOf(command.id, values, command, connections) : undefined}
          >
            Same as: {values ? lineOf(command.id, values, command, connections) : "…"}
          </p>
        )
      }
      onSubmit={async (values, drafts) => {
        if (!command) throw new Error("Pick a command first.")
        const data = await queue.mutateAsync({ sessionId, command: command.id, args: Object.keys(values).length > 0 ? values : null })
        // A stream picked here goes now, as the task waits for it.
        for (const d of Object.values(drafts)) {
          if (d.kind === "stream" && d.file) startStreamUpload(data.queueMinionTask.id, d.file)
        }
        onQueued?.(data.queueMinionTask)
      }}
      idPrefix={`task-${sessionId}`}
    />
  )
}

/** The command form's line on a command that works over some kinds only. */
function KindsLine({ kinds, connKind }: { kinds?: readonly string[]; connKind?: string | null }) {
  const note = commandKindsNote(kinds, connKind)
  if (!note) return null
  return <span className={cn("mt-1 block", note.blocked && "text-amber-700 dark:text-amber-400")}>{note.text}.</span>
}

/** A composite command's line in its form: what its factory's code runs as its subtasks. */
function CompositeLine({ uses }: { uses?: readonly string[] }) {
  if (!uses?.length) return null
  return <span className="mt-1 block">Composite: its factory&apos;s code runs {uses.join(", ")} as its subtasks.</span>
}

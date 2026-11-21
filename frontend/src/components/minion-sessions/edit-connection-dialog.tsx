import { useMemo } from "react"
import { toast } from "sonner"
import { CommandFormDialog } from "@/components/command-form/command-form-dialog"
import { initialParamDrafts, type Draft } from "@/components/modules/management/field-values"
import { useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { CONNECTION_FORMAT, connectionJSON, type ConnectionChoice } from "./connection-choices"
import { useEditConnectionCommand } from "./use-connection-choices"
import { ConnectionChoicesProvider } from "./session-connection-choices"

// Editing one of a session's connections (docs: contracts/task-results-and-actions,
// edit_connection): the session's command for core's edit_connection action,
// its form opened with the connection's place in its int connection param
// (conn) and the connection itself, as JSON, in its structured one
// (connection), to edit there. Run queues it, a task like any other; the
// minion says its connections again once it took it.

export interface EditConnectionDialogProps {
  sessionId: string
  /** The connection to edit; null: closed. */
  connection: ConnectionChoice | null
  connections: readonly ConnectionChoice[]
  onClose: () => void
}

export function EditConnectionDialog({ sessionId, connection: c, connections, onClose }: EditConnectionDialogProps) {
  const command = useEditConnectionCommand(sessionId)
  const queue = useQueueMinionTask()
  const fields = useMemo(() => command?.fields ?? [], [command])
  const drafts = useMemo(() => {
    const out: Record<string, Draft> = initialParamDrafts(fields)
    if (!c) return out
    for (const f of fields) {
      if (f.format !== CONNECTION_FORMAT) continue
      if (f.type === "int") out[f.key] = { kind: "text", text: String(c.place) }
      if (f.type === "structured" && c.value) out[f.key] = { kind: "json", text: connectionJSON(c.value) }
    }
    return out
  }, [fields, c])
  if (!command) return null
  return (
    <ConnectionChoicesProvider choices={connections}>
      <CommandFormDialog
        open={c !== null}
        onOpenChange={(o) => !o && onClose()}
        title={`Edit ${c?.name ?? "the connection"}`}
        description={
          <>
            <code className="font-mono text-xs">{command.id}</code>: the minion takes the connection below in place of this one, and says its
            connections again.
          </>
        }
        fields={fields}
        initialDrafts={drafts}
        formKey={`${command.id}-${c?.place ?? 0}`}
        submitLabel="Queue"
        idPrefix={`edit-connection-${sessionId}`}
        onSubmit={async (values) => {
          await queue.mutateAsync({ sessionId, command: command.id, args: Object.keys(values).length > 0 ? values : null })
          toast.success(`Edit of ${c?.name ?? "the connection"} queued (${command.id})`)
        }}
      />
    </ConnectionChoicesProvider>
  )
}

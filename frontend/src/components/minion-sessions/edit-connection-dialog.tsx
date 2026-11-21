import { useMemo } from "react"
import { toast } from "sonner"
import { initialParamDrafts, type Draft } from "@/components/modules/management/field-values"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { withConnection, type ConnectionChoice } from "./connection-choices"
import { useEditConnectionCommand } from "./use-connection-choices"
import { ConnectionChoicesProvider } from "./session-connection-choices"
import { SessionCommandDialog } from "./session-command-dialog"

// Editing one of a session's connections (docs: contracts/task-results-and-actions,
// edit_connection): the session's command for core's edit_connection action,
// in the console's form (SessionCommandDialog), opened with the
// connection's place in its int connection param (conn) and the connection
// itself, as JSON, in its structured one (connection), to edit there. Run
// queues it, a task like any other; the minion says its connections again
// once it took it.

export interface EditConnectionDialogProps {
  sessionId: string
  /** The connection to edit; null: closed. */
  connection: ConnectionChoice | null
  connections: readonly ConnectionChoice[]
  onClose: () => void
}

export function EditConnectionDialog({ sessionId, connection: c, connections, onClose }: EditConnectionDialogProps) {
  const command = useEditConnectionCommand(sessionId)
  const connKind = useMinionSession(sessionId).data?.minionSession?.connKind
  const drafts = useMemo(() => {
    const fields = command?.fields ?? []
    const out: Record<string, Draft> = initialParamDrafts(fields)
    return c ? withConnection(fields, out, c) : out
  }, [command, c])
  if (!command) return null
  return (
    <ConnectionChoicesProvider choices={connections}>
      <SessionCommandDialog
        sessionId={sessionId}
        command={command}
        open={c !== null}
        onOpenChange={(o) => !o && onClose()}
        initialDrafts={drafts}
        formKey={`${command.id}-${c?.place ?? 0}`}
        connections={connections}
        connKind={connKind}
        onQueued={() => toast.success(`Edit of ${c?.name ?? "the connection"} queued (${command.id})`)}
      />
    </ConnectionChoicesProvider>
  )
}

import { PlugIcon, UnplugIcon } from "lucide-react"
import { toast } from "sonner"
import { ContextMenuItem } from "@/components/ui/context-menu"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { errorMessage } from "@/components/modules/management/api-errors"

// Connect / Disconnect in a session row's menu, for a session whose minion
// listens (bind-persistent): it runs core's connect or disconnect command,
// a task like any other, so the session's history says who connected when.
// Which one shows follows the session's connected; the task's result is in
// the session's console (the toast opens it).

export function ConnectTargetMenu({
  s,
  canWrite,
  onInteract,
}: {
  s: MinionSessionFieldsFragment
  canWrite: boolean
  /** Opens the session in the work pane, where the task's result shows. */
  onInteract: () => void
}) {
  const queue = useQueueMinionTask()
  if (s.connKind !== "bind-persistent") return null
  const connected = s.connected
  const command = connected ? "disconnect" : "connect"
  const why = !canWrite ? "Needs operator in this operation" : s.state !== "OPEN" ? "The session is closed" : undefined

  async function run() {
    // The menu closes on the click: the toast says it went, the console how.
    try {
      await queue.mutateAsync({ sessionId: s.id, command, args: null })
      toast.success(`Session #${s.number}: ${command} queued`, {
        description: connected ? "Core closes the connection." : "Core asks the channel to dial the minion.",
        action: { label: "Interact", onClick: onInteract },
      })
    } catch (err) {
      toast.error(connected ? "Could not disconnect" : "Could not connect", {
        description: errorMessage(err, "Request failed"),
      })
    }
  }

  return (
    <ContextMenuItem
      disabled={why !== undefined}
      title={why ?? `Runs ${command}: core's own command, kept in the session's tasks`}
      onClick={() => void run()}
    >
      {connected ? <UnplugIcon /> : <PlugIcon />}
      {connected ? "Disconnect" : "Connect"}
    </ContextMenuItem>
  )
}

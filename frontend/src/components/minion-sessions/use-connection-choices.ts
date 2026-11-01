import { createContext, useContext, useMemo } from "react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useMinionSessionCommands, useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { actionCommands, chooseActionCommand, readPicked } from "./action-commands"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { settingsSummary } from "@/components/builds/build-values"
import { useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSession, useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import { connectionText, type ConnectionChoice } from "./connection-choices"

/**
 * A session's connections as choices (connection-choices.ts): its build's,
 * with each one's address and the one the minion uses now. Empty while
 * loading, and for a session whose build is gone.
 */
export function useSessionConnectionChoices(sessionId: string): ConnectionChoice[] {
  const { data } = useMinionSessionConnections(sessionId)
  const session = useMinionSession(sessionId).data?.minionSession
  const kinds = useChannelKinds()
  const conn = session?.conn ?? null
  return useMemo(() => {
    const channels = kindsByName(kinds.data?.channelKinds ?? [])
    return (data?.minionSession?.connections ?? []).map((c) => ({
      place: c.place,
      name: c.name,
      kind: c.kind,
      address: settingsSummary(channels.get(c.moduleName), c.kind, c.params, 1) || undefined,
      inUse: c.place === conn,
    }))
  }, [data, kinds.data, conn])
}

/** The session's connections the command forms under ConnectionChoicesProvider offer. */
export const ConnectionChoicesContext = createContext<readonly ConnectionChoice[]>([])

/** The connections a connection param offers here; none outside a session's forms. */
export function useConnectionChoices(): readonly ConnectionChoice[] {
  return useContext(ConnectionChoicesContext)
}

const SWITCH = "switch_connection"

/**
 * Switching a session's connection: the session's command for core's
 * switch_connection action (the one picked for the session, else the
 * factory's first), undefined when its minion has none; and switchTo, which
 * queues it for a connection, saying so in a toast.
 */
export function useSwitchConnection(sessionId: string) {
  const commands = useMinionSessionCommands(sessionId)
  const command = chooseActionCommand(actionCommands(commands.data?.minionSessionCommands ?? [], SWITCH), readPicked(sessionId, SWITCH))
  const queue = useQueueMinionTask()
  async function switchTo(place: number, connections: readonly ConnectionChoice[] = []) {
    if (!command) return
    const name = connections.length > 0 ? connectionText(place, connections) : String(place)
    try {
      await queue.mutateAsync({ sessionId, command: command.id, args: { conn: place } })
      toast.success(`Switch to ${name} queued (${command.id})`)
    } catch (err) {
      toast.error(errorMessage(err, "Failed to queue the switch"))
    }
  }
  return { command, loading: commands.isLoading, pending: queue.isPending, switchTo }
}

import { createContext, useContext, useMemo } from "react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useMinionSessionCommands, useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { actionCommands, chooseActionCommand, readPicked } from "./action-commands"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { settingsSummary } from "@/components/builds/build-values"
import { useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSession, useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import { choiceTitle, type ConnectionChoice } from "./connection-choices"
import { useConsoleCommands } from "./console-commands"

/**
 * A session's connections as choices (connection-choices.ts): its own, else
 * its build's, with each one's address and the one the minion uses now. Empty while
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
      channel: c.moduleName,
      kind: c.kind,
      address: settingsSummary(channels.get(c.moduleName), c.kind, c.params) || undefined,
      inUse: c.place === conn,
      value: {
        channel: c.moduleName,
        kind: c.kind,
        params: isRecord(c.params) ? c.params : undefined,
        values: isRecord(c.values) ? c.values : undefined,
      },
    }))
  }, [data, kinds.data, conn])
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

/** The session's connections the command forms under ConnectionChoicesProvider offer. */
export const ConnectionChoicesContext = createContext<readonly ConnectionChoice[]>([])

/** The connections a connection param offers here; none outside a session's forms. */
export function useConnectionChoices(): readonly ConnectionChoice[] {
  return useContext(ConnectionChoicesContext)
}

const SWITCH = "switch_connection"
const EDIT = "edit_connection"
const ADD = "add_connection"
const REMOVE = "remove_connection"

/** The session's command for remove_connection: the one picked for the session, else the factory's first; undefined for none. */
export function useRemoveConnectionCommand(sessionId: string) {
  const { commands } = useConsoleCommands(sessionId)
  return chooseActionCommand(actionCommands(commands, REMOVE), readPicked(sessionId, REMOVE))
}

/** The session's command for add_connection: the one picked for the session, else the factory's first; undefined for none. */
export function useAddConnectionCommand(sessionId: string) {
  const { commands } = useConsoleCommands(sessionId)
  return chooseActionCommand(actionCommands(commands, ADD), readPicked(sessionId, ADD))
}

/** The session's command for edit_connection: the one picked for the session, else the factory's first; undefined for none. */
export function useEditConnectionCommand(sessionId: string) {
  const { commands } = useConsoleCommands(sessionId)
  return chooseActionCommand(actionCommands(commands, EDIT), readPicked(sessionId, EDIT))
}

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
    const c = connections.find((x) => x.place === place)
    try {
      await queue.mutateAsync({ sessionId, command: command.id, args: { conn: place } })
      toast.success(`Switch to connection ${c ? choiceTitle(c) : place} queued (${command.id})`)
    } catch (err) {
      toast.error(errorMessage(err, "Failed to queue the switch"))
    }
  }
  return { command, loading: commands.isLoading, pending: queue.isPending, switchTo }
}

import { createContext, useContext, useMemo } from "react"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { settingsSummary } from "@/components/builds/build-values"
import { useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSession, useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import type { ConnectionChoice } from "./connection-choices"

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

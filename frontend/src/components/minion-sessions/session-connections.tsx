import { useMemo } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { IdRows, InfoCard, NoteRow } from "@/components/ui/info-card"
import { kindLabels, kindsByName } from "@/components/endpoints/endpoint-values"
import { settingsSummary } from "@/components/builds/build-values"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import { useMinionSessionCommands, useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { actionCommands, chooseActionCommand, readPicked } from "./action-commands"

// A session's connections (docs: contracts/channel-core-connections, Connection
// management): its build's, in the order the minion tries them, the one it
// uses marked, as its hello or its last conn_update said; and the moves it
// reported. Switch to runs the session's command for core's
// switch_connection action, a task like any other: the minion moves once the
// new connection answers, and says so.

type Session = MinionSessionFieldsFragment

const SWITCH = "switch_connection"

export function ConnectionsCard({ s }: { s: Session }) {
  const { data } = useMinionSessionConnections(s.id)
  const connections = data?.minionSession?.connections ?? []
  const kinds = useChannelKinds()
  const channels = useMemo(() => kindsByName(kinds.data?.channelKinds ?? []), [kinds.data])
  const commands = useMinionSessionCommands(s.id).data?.minionSessionCommands ?? []
  const command = chooseActionCommand(actionCommands(commands, SWITCH), readPicked(s.id, SWITCH))
  const queue = useQueueMinionTask()
  const open = s.state === "OPEN"

  async function switchTo(place: number) {
    if (!command) return
    try {
      await queue.mutateAsync({ sessionId: s.id, command: command.id, args: { conn: place } })
      toast.success(`Switch to connection ${place} queued (${command.id})`)
    } catch (err) {
      toast.error(errorMessage(err, "Failed to queue the switch"))
    }
  }

  return (
    <InfoCard
      title="Connections"
      note={s.conn ? `using ${s.conn} of ${connections.length}` : connections.length || undefined}
    >
      {connections.length === 0 ? (
        <NoteRow>{data ? "The build is deleted: its connections are not known." : "Loading…"}</NoteRow>
      ) : (
        <IdRows
          rows={connections.map((c) => {
            const address = settingsSummary(channels.get(c.moduleName), c.kind, c.params, 1)
            const inUse = c.place === s.conn
            return {
              id: String(c.place),
              content: (
                <div className="flex items-baseline gap-2">
                  <div className="min-w-0 flex-1">
                    <span className="font-mono">{c.name}</span>
                    <span className="text-muted-foreground" title={kindLabels[c.kind]}>
                      {" "}
                      · {c.moduleName} · {c.kind}
                    </span>
                    {inUse && <span className="ml-2 rounded-md border px-1.5 py-0.5 text-xs">in use</span>}
                    {address && <div className="truncate font-mono text-xs text-muted-foreground">{address}</div>}
                  </div>
                  {command && open && !inUse && (
                    <Button
                      type="button"
                      variant="outline"
                      size="xs"
                      disabled={queue.isPending}
                      title={`Queue ${command.id} with conn ${c.place}: the minion moves once this connection answers`}
                      onClick={() => switchTo(c.place)}
                    >
                      Switch to
                    </Button>
                  )}
                </div>
              ),
            }
          })}
        />
      )}
      {connections.length > 1 && !command && (
        <NoteRow>The minion was built without a command to switch connections.</NoteRow>
      )}
    </InfoCard>
  )
}

import { useMemo, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { IdRows, InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { kindLabels, kindsByName } from "@/components/endpoints/endpoint-values"
import { settingsSummary } from "@/components/builds/build-values"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSessionConnections, useMinionSessions } from "@/graphql/hooks/minion-sessions"
import {
  useBindConnections,
  useCheckMinionTarget,
  useConnectMinionTarget,
  useDisconnectMinionTarget,
  useMinionTargetChangedSubscription,
  useSessionTarget,
} from "@/graphql/hooks/minion-targets"
import { useRecordModals } from "@/hooks/use-record-modals"
import { errorMessage } from "@/components/modules/management/api-errors"
import { TargetStateBadge } from "@/components/targets/target-state-badge"
import { TARGET_STATE_LABEL, isPersistent, problemsText, shownState, type Target } from "@/components/targets/target-values"
import { TargetDialog } from "@/components/targets/target-dialog"
import { useSwitchConnection } from "./use-connection-choices"

// A session's connections (docs: contracts/channel-core-connections, Connection
// management): its build's, in the order the minion tries them, the one it
// uses marked, as its hello or its last conn_update said; and the moves it
// reported. Switch to runs the session's command for core's
// switch_connection action, a task like any other: the minion moves once the
// new connection answers, and says so. A bind-call session (a minion core
// calls) shows its target under its connection: where the minion is, what
// the latest call came to, and Check now; one whose target was removed
// says core cannot reach it, and a target added there takes the session
// over. A connection the minion listens on (bind-persistent) offers a target
// even while the minion uses another: core dials it once the minion
// switches to it. A session whose latest message came through delegates
// has a via row: each delegate's session, nearest first, the task that
// carried it and since when (the session's own fields: live).

type Session = MinionSessionFieldsFragment

export function ConnectionsCard({ s }: { s: Session }) {
  const { data } = useMinionSessionConnections(s.id)
  const connections = data?.minionSession?.connections ?? []
  const kinds = useChannelKinds()
  const channels = useMemo(() => kindsByName(kinds.data?.channelKinds ?? []), [kinds.data])
  const { command, pending, switchTo } = useSwitchConnection(s.id)
  const open = s.state === "OPEN"
  const targetQuery = useSessionTarget(s.id)
  const target = targetQuery.data ?? null
  const bind = s.connKind === "bind-call" || s.connKind === "bind-persistent"
  useMinionTargetChangedSubscription(target || bind ? s.operationId : "")

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
                    {target && target.conn === c.place && <TargetLine target={target} />}
                    {!target && targetQuery.isSuccess && ((bind && inUse) || c.kind === "bind-persistent") && (
                      <NoTargetLine s={s} place={c.place} open={open} inUse={inUse} />
                    )}
                  </div>
                  {command && open && !inUse && (
                    <Button
                      type="button"
                      variant="outline"
                      size="xs"
                      disabled={pending}
                      title={
                        c.kind === "bind-persistent"
                          ? `Queue ${command.id} with conn ${c.place}: the minion opens its port, and moves once core dials it (its target)`
                          : `Queue ${command.id} with conn ${c.place}: the minion moves once this connection answers`
                      }
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
      <ViaRow s={s} />
      {connections.length > 1 && !command && (
        <NoteRow>The minion was built without a command to switch connections.</NoteRow>
      )}
    </InfoCard>
  )
}

/** The delegates the session's latest message came through, nearest first: each one's session (opens it), then the task that carried it and since when. */
function ViaRow({ s }: { s: Session }) {
  const modals = useRecordModals()
  // Their hosts; null: removed.
  const hops = useMinionSessions(s.via.map((v) => v.sessionId))
  if (s.via.length === 0) return null
  const task = s.via[0].taskId
  return (
    <LabelRow label="via">
      <div className="flex flex-wrap items-baseline gap-x-1.5">
        {s.via.map((v, i) => (
          <span key={v.sessionId} className="flex items-baseline gap-1.5">
            {i > 0 && <span className="text-muted-foreground">→</span>}
            <button
              type="button"
              className="font-mono underline-offset-2 hover:underline"
              title={`Open session #${v.number}`}
              onClick={() => modals.open({ kind: "session", id: v.sessionId })}
            >
              #{v.number}
            </button>
            {hops[i]?.host && <span className="text-muted-foreground">{hops[i].host}</span>}
            {hops[i] === null && <span className="text-muted-foreground italic">removed</span>}
          </span>
        ))}
      </div>
      <div className="text-xs text-muted-foreground">
        delegate task{" "}
        <code className="font-mono" title={task}>
          {task.slice(0, 8)}
        </code>
        {s.viaSince && (
          <>
            {" "}
            · since <FormattedDateTimeText date={s.viaSince} />
          </>
        )}
      </div>
    </LabelRow>
  )
}

/** The minion's target under its connection: where it is, its state, and Check now. */
function TargetLine({ target: t }: { target: Target }) {
  const check = useCheckMinionTarget()
  const connect = useConnectMinionTarget()
  const disconnect = useDisconnectMinionTarget()
  const modals = useRecordModals()
  const [busy, setBusy] = useState(false)
  // A connection kept open: Connect or Disconnect it; a bind-call minion: Check now.
  const persistent = isPersistent(t)
  const connected = t.state === "connected"
  async function checkNow() {
    setBusy(true)
    try {
      const after = !persistent
        ? (await check.mutateAsync(t.id)).checkMinionTarget
        : connected
          ? (await disconnect.mutateAsync(t.id)).disconnectMinionTarget
          : (await connect.mutateAsync(t.id)).connectMinionTarget
      const label = TARGET_STATE_LABEL[after.state] ?? after.state
      if (after.state === "connected" || (persistent && connected && after.state === "not_connected")) toast.success(`${after.name}: ${label}`)
      else toast.warning(`${after.name}: ${label}`, { description: after.stateDetail ?? undefined })
    } catch (err) {
      toast.error("Could not call the target", { description: errorMessage(err, "Request failed") })
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
      <span className="text-muted-foreground">target</span>
      <span className="min-w-0 truncate font-mono" title={t.name}>
        {t.name}
      </span>
      <TargetStateBadge state={shownState(t)} pending={busy} />
      {t.delegate && <span className="text-muted-foreground">through session #{t.delegate.number}</span>}
      {(t.problems.length > 0 || t.stateDetail) && (
        <span className="min-w-0 truncate text-muted-foreground" title={problemsText(t) || t.stateDetail || undefined}>
          {problemsText(t) || t.stateDetail}
        </span>
      )}
      <Button type="button" variant="outline" size="xs" disabled={busy} onClick={checkNow}>
        {persistent ? (connected ? "Disconnect" : "Connect") : "Check now"}
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        onClick={() => modals.open({ kind: "build", id: t.buildId, tab: "targets" })}
      >
        Targets
      </Button>
    </div>
  )
}

/**
 * A bind-call session with no target (it was removed): core cannot reach
 * the minion. A target added here takes the session over.
 */
function NoTargetLine({ s, place, open, inUse }: { s: Session; place: number; open: boolean; inUse: boolean }) {
  const [adding, setAdding] = useState(false)
  const conns = useBindConnections(adding ? s.buildId : "").data ?? []
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
      <span className="text-muted-foreground">
        {inUse
          ? "No target: core cannot reach this minion. Tasks queued for it wait."
          : "No target: core cannot dial this connection, so a switch to it would wait in vain."}
      </span>
      {open && (
        <Button type="button" variant="outline" size="xs" onClick={() => setAdding(true)}>
          Add target
        </Button>
      )}
      <TargetDialog
        open={adding && conns.length > 0}
        onClose={() => setAdding(false)}
        operationId={s.operationId}
        buildId={s.buildId}
        connections={conns.filter((c) => c.place === place)}
        target={null}
        session={{ id: s.id, number: s.number }}
      />
    </div>
  )
}

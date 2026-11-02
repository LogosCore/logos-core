import { useMemo, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { IdRows, InfoCard, NoteRow } from "@/components/ui/info-card"
import { kindLabels, kindsByName } from "@/components/endpoints/endpoint-values"
import { settingsSummary } from "@/components/builds/build-values"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import {
  useBindConnections,
  useCheckMinionTarget,
  useMinionTargetChangedSubscription,
  useSessionTarget,
} from "@/graphql/hooks/minion-targets"
import { useRecordModals } from "@/hooks/use-record-modals"
import { errorMessage } from "@/components/modules/management/api-errors"
import { TargetStateBadge } from "@/components/targets/target-state-badge"
import { TARGET_STATE_LABEL, problemsText, shownState, type Target } from "@/components/targets/target-values"
import { TargetDialog } from "@/components/targets/target-dialog"
import { useSwitchConnection } from "./use-connection-choices"

// A session's connections (docs: contracts/channel-core-connections, Connection
// management): its build's, in the order the minion tries them, the one it
// uses marked, as its hello or its last conn_update said; and the moves it
// reported. Switch to runs the session's command for core's
// switch_connection action, a task like any other: the minion moves once the
// new connection answers, and says so. A bind-call session (a page core
// calls) shows its target under its connection: where the page is, what
// the latest call came to, and Check now; one whose target was removed
// says core cannot reach it, and a target added there takes the session
// over.

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
  const bind = s.connKind === "bind-call"
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
                    {!target && targetQuery.isSuccess && bind && inUse && <NoTargetLine s={s} place={c.place} open={open} />}
                  </div>
                  {command && open && !inUse && (
                    <Button
                      type="button"
                      variant="outline"
                      size="xs"
                      disabled={pending}
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

/** The page's target under its connection: its URL, state, and Check now. */
function TargetLine({ target: t }: { target: Target }) {
  const check = useCheckMinionTarget()
  const modals = useRecordModals()
  const [busy, setBusy] = useState(false)
  async function checkNow() {
    setBusy(true)
    try {
      const after = (await check.mutateAsync(t.id)).checkMinionTarget
      const label = TARGET_STATE_LABEL[after.state] ?? after.state
      if (after.state === "connected") toast.success(`${after.name}: ${label}`)
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
      {(t.problems.length > 0 || t.stateDetail) && (
        <span className="min-w-0 truncate text-muted-foreground" title={problemsText(t) || t.stateDetail || undefined}>
          {problemsText(t) || t.stateDetail}
        </span>
      )}
      <Button type="button" variant="outline" size="xs" disabled={busy} onClick={checkNow}>
        Check now
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
 * the page. A target added here takes the session over.
 */
function NoTargetLine({ s, place, open }: { s: Session; place: number; open: boolean }) {
  const [adding, setAdding] = useState(false)
  const conns = useBindConnections(adding ? s.buildId : "").data ?? []
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
      <span className="text-muted-foreground">No target: core cannot reach this page. Tasks queued for it wait.</span>
      {open && (
        <Button type="button" variant="outline" size="xs" onClick={() => setAdding(true)}>
          Add target
        </Button>
      )}
      <TargetDialog
        open={adding && conns.length > 0}
        onClose={() => setAdding(false)}
        buildId={s.buildId}
        connections={conns.filter((c) => c.place === place)}
        target={null}
        session={{ id: s.id, number: s.number }}
      />
    </div>
  )
}

import { useMemo, useState } from "react"
import { toast } from "sonner"
import { PlusIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { IdRows, NoteRow, SectionRow } from "@/components/ui/info-card"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { ConnectionFoldCard } from "@/components/builds/connection-fold-card"
import { factoryKindsByName, paramFields, settingFields, settingsSummary } from "@/components/builds/build-values"
import { valueRows, type ValueRow } from "@/components/builds/build-record"
import { rowsOf } from "@/components/builds/build-value-rows"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useBuildFiles, useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
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
import {
  useAddConnectionCommand,
  useEditConnectionCommand,
  useRemoveConnectionCommand,
  useSessionConnectionChoices,
  useSwitchConnection,
} from "./use-connection-choices"
import { withConnection } from "./connection-choices"
import { initialParamDrafts } from "@/components/modules/management/field-values"
import { ConnectionChoicesProvider } from "./session-connection-choices"
import { SessionCommandDialog } from "./session-command-dialog"
import { EditConnectionDialog } from "./edit-connection-dialog"

// A session's connections (docs: contracts/channel-core-connections, Connection
// management): the minion's own when its hello said them, else its build's,
// in the order the minion tries them, a card each. Its head says what it is
// (channel, name, kind, where it connects), marks the one in use (as its
// hello or its last conn_update said), and has its actions: Edit (the
// session's command for edit_connection) and Switch to (for
// switch_connection), each a task like any other. Unfolded (the one in use
// starts so), it lists the channel's settings set and the factory's values.
// A bind-call session (a minion core calls) shows its target in the head of
// its connection: where the minion is, what the latest call came to, and
// Check now; one whose target was removed says core cannot reach it, and a
// target added there takes the session over. A connection the minion
// listens on (bind-persistent) offers a target even while the minion uses
// another: core dials it once the minion switches to it. A session whose
// latest message came through delegates says so in the head of the one in
// use: each delegate's session, nearest first, the task that carried it and
// since when (the session's own fields: live).

type Session = MinionSessionFieldsFragment

export function ConnectionCards({ s }: { s: Session }) {
  const { data } = useMinionSessionConnections(s.id)
  const connections = data?.minionSession?.connections ?? []
  const kinds = useChannelKinds()
  const channels = useMemo(() => kindsByName(kinds.data?.channelKinds ?? []), [kinds.data])
  const factoryKinds = useFactoryKinds()
  const factory = useMemo(() => factoryKindsByName(factoryKinds.data?.factoryKinds ?? []).get(s.moduleName), [factoryKinds.data, s.moduleName])
  const files = useBuildFiles(s.operationId)
  const fileNames = useMemo(() => new Map((files.data?.buildFiles ?? []).map((f) => [f.id, f.name])), [files.data])
  const channelIcons = useChannelIcons()
  const { command, pending, switchTo } = useSwitchConnection(s.id)
  // Edit: the session's command for edit_connection, opened on a connection.
  const editCommand = useEditConnectionCommand(s.id)
  const choices = useSessionConnectionChoices(s.id)
  const [editing, setEditing] = useState<number | null>(null)
  // Add connection: the session's command for add_connection, its
  // connection empty to fill (or filled from one of the session's).
  const addCommand = useAddConnectionCommand(s.id)
  const [adding, setAdding] = useState(false)
  // Remove: the session's command for remove_connection, opened on a
  // connection (its form is the confirmation). Never the one in use, the
  // only one, or the one its target names: core refuses those.
  const removeCommand = useRemoveConnectionCommand(s.id)
  const [removing, setRemoving] = useState<number | null>(null)
  // The cards unfolded, by place; the one in use to start with.
  const [unfolded, setUnfolded] = useState<ReadonlySet<number>>(() => new Set(s.conn ? [s.conn] : []))
  const open = s.state === "OPEN"
  const targetQuery = useSessionTarget(s.id)
  const target = targetQuery.data ?? null
  const bind = s.connKind === "bind-call" || s.connKind === "bind-persistent"
  useMinionTargetChangedSubscription(target || bind ? s.operationId : "")

  if (connections.length === 0) {
    return (
      <section className="min-w-0 rounded-xl border bg-card px-4 py-3 text-sm text-muted-foreground">
        {data ? "The build is deleted: its connections are not known." : "Loading…"}
      </section>
    )
  }
  const toggle = (place: number) =>
    setUnfolded((was) => {
      const next = new Set(was)
      if (next.has(place)) next.delete(place)
      else next.add(place)
      return next
    })

  return (
    <>
      {connections.map((c) => {
        const inUse = c.place === s.conn
        const expanded = unfolded.has(c.place)
        const address = settingsSummary(channels.get(c.moduleName), c.kind, c.params)
        const settings = valueRows(settingFields(channels.get(c.moduleName), c.kind), c.params).filter(isSet)
        const own = valueRows(factory ? paramFields(factory, c.moduleName, c.kind) : [], c.values).filter(isSet)
        return (
          <ConnectionFoldCard
            key={c.place}
            place={c.place}
            moduleName={c.moduleName}
            kind={c.kind}
            icon={channelIcons.get(c.moduleName)}
            badge={inUse && <span className="shrink-0 rounded-md border px-1.5 text-xs">in use</span>}
            expanded={expanded}
            onToggle={() => toggle(c.place)}
            actions={
              <>
                {editCommand && open && (
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    title={`Queue ${editCommand.id} for connection ${c.place}: change its settings first`}
                    onClick={() => setEditing(c.place)}
                  >
                    Edit
                  </Button>
                )}
                {removeCommand && open && !inUse && connections.length > 1 && !(target && target.conn === c.place) && (
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    title={`Queue ${removeCommand.id} for connection ${c.place}: the minion takes it out, the ones after it moving up`}
                    onClick={() => setRemoving(c.place)}
                  >
                    Remove
                  </Button>
                )}
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
              </>
            }
            lines={
              <>
                {address && (
                  // The head says its channel already: a session's connections have no names.
                  <div className="truncate font-mono text-xs text-muted-foreground" title={address}>
                    {address}
                  </div>
                )}
                {target && target.conn === c.place && <TargetLine target={target} />}
                {!target && targetQuery.isSuccess && ((bind && inUse) || c.kind === "bind-persistent") && (
                  <NoTargetLine s={s} place={c.place} open={open} inUse={inUse} />
                )}
                {inUse && <ViaLine s={s} />}
                {inUse && connections.length > 1 && !command && (
                  <p className="text-xs text-muted-foreground">The minion was built without a command to switch connections.</p>
                )}
              </>
            }
          >
            {own.length > 0 && <SectionRow title="Settings" note={c.moduleName} />}
            {settings.length === 0 ? <NoteRow>No settings set.</NoteRow> : <IdRows rows={rowsOf(settings, fileNames)} />}
            {own.length > 0 && (
              <>
                <SectionRow title={`${s.moduleName}'s values`} note={`for ${c.moduleName}`} />
                <IdRows rows={rowsOf(own, fileNames)} />
              </>
            )}
          </ConnectionFoldCard>
        )
      })}
      {/* Where a new connection lands: after the others, as the build form adds one. */}
      {addCommand && open && (
        <button
          type="button"
          className="flex min-h-11 w-full items-center justify-center gap-1.5 rounded-xl border border-dashed text-sm text-muted-foreground transition-colors hover:border-foreground/30 hover:bg-card hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          title={`Queue ${addCommand.id}: the minion takes a connection it did not have, after its others`}
          onClick={() => setAdding(true)}
        >
          <PlusIcon className="size-4" />
          Add connection
        </button>
      )}
      <EditConnectionDialog
        sessionId={s.id}
        connection={choices.find((x) => x.place === editing) ?? null}
        connections={choices}
        onClose={() => setEditing(null)}
      />
      {removeCommand && (
        <ConnectionChoicesProvider choices={choices}>
          <SessionCommandDialog
            sessionId={s.id}
            command={removeCommand}
            open={removing !== null}
            onOpenChange={(o) => !o && setRemoving(null)}
            initialDrafts={(() => {
              const picked = choices.find((x) => x.place === removing)
              const drafts = initialParamDrafts(removeCommand.fields)
              return picked ? withConnection(removeCommand.fields, drafts, picked) : drafts
            })()}
            formKey={`${removeCommand.id}-${removing ?? 0}`}
            connections={choices}
            connKind={s.connKind}
            onQueued={() => toast.success(`Removal of connection ${removing} queued (${removeCommand.id})`)}
          />
        </ConnectionChoicesProvider>
      )}
      {addCommand && (
        <ConnectionChoicesProvider choices={choices}>
          <SessionCommandDialog
            sessionId={s.id}
            command={addCommand}
            open={adding}
            onOpenChange={setAdding}
            formKey={`${addCommand.id}-${adding}`}
            connections={choices}
            connKind={s.connKind}
            onQueued={() => toast.success(`Connection queued to add (${addCommand.id})`)}
          />
        </ConnectionChoicesProvider>
      )}
    </>
  )
}

/** A row with a value: one not set says nothing about the connection. */
function isSet(r: ValueRow): boolean {
  return r.value !== undefined
}

/** The delegates the session's latest message came through, nearest first: each one's session (opens it), then the task that carried it and since when. */
function ViaLine({ s }: { s: Session }) {
  const modals = useRecordModals()
  // Their hosts; null: removed.
  const hops = useMinionSessions(s.via.map((v) => v.sessionId))
  if (s.via.length === 0) return null
  const task = s.via[0].taskId
  return (
    <div className="mt-1 flex flex-wrap items-baseline gap-x-1.5 text-xs">
      <span className="text-muted-foreground">via</span>
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
      <span className="text-muted-foreground">
        · delegate task{" "}
        <code className="font-mono" title={task}>
          {task.slice(0, 8)}
        </code>
        {s.viaSince && (
          <>
            {" "}
            · since <FormattedDateTimeText date={s.viaSince} />
          </>
        )}
      </span>
    </div>
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

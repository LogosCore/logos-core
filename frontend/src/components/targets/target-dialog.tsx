import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { SettingRow, SettingsSection } from "@/components/ui/settings-rows"
import { FieldControl } from "@/components/modules/management/field-control"
import {
  evaluateParams,
  groupByField,
  initialDraft,
  type Draft,
  type FieldDef,
} from "@/components/modules/management/field-values"
import { errorMessage } from "@/components/modules/management/api-errors"
import { problemsFromError } from "@/components/endpoints/endpoint-values"
import type { BuildBindConnectionsQuery } from "@/graphql/gql/graphql"
import { useAddMinionTarget, useUpdateMinionTarget } from "@/graphql/hooks/minion-targets"
import { TARGET_POLICIES, TARGET_POLICY_HELP, TARGET_POLICY_LABEL, targetValues, type Target } from "./target-values"
import { DelegatePicker, type DelegateSession } from "./delegate-picker"

export type BindConnection = NonNullable<BuildBindConnectionsQuery["build"]>["bindConnections"][number]

/** A session a new target takes over: one whose target was removed. */
export interface TakenOverSession {
  id: string
  number: number
}

/**
 * Adds a target to a build's bind connection, or edits one: the channel's
 * target fields (where the minion is, how to reach it) and whether core
 * retries an unreachable one. Core calls it as soon as it is saved. Added
 * for a session (one whose target was removed), it takes that session over:
 * the minion is called under the session's id and the session goes on.
 */
export function TargetDialog({
  open,
  onClose,
  operationId,
  buildId,
  connections,
  target,
  session,
}: {
  open: boolean
  onClose: () => void
  operationId: string
  buildId: string
  /** The build's bind connections: what a new target can be of. */
  connections: readonly BindConnection[]
  /** The target to edit; null adds one. */
  target: Target | null
  /** The session a new target takes over; none for a new session. */
  session?: TakenOverSession
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        {/* Keyed so each opening starts from the target as it is then. */}
        {open && (
          <TargetForm
            key={target?.id ?? "new"}
            operationId={operationId}
            buildId={buildId}
            connections={connections}
            target={target}
            session={session}
            onClose={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function draftsFor(fields: readonly FieldDef[], values: Record<string, unknown>): Record<string, Draft> {
  const out: Record<string, Draft> = {}
  for (const f of fields) out[f.key] = initialDraft(f, values[f.key] ?? null, { secretAsText: true })
  return out
}

function TargetForm({
  operationId,
  buildId,
  connections,
  target,
  session,
  onClose,
}: {
  operationId: string
  buildId: string
  connections: readonly BindConnection[]
  target: Target | null
  session?: TakenOverSession
  onClose: () => void
}) {
  const add = useAddMinionTarget()
  const update = useUpdateMinionTarget()
  const usable = connections.filter((c) => !c.problem)
  const [place, setPlace] = useState<number>(target?.conn ?? usable[0]?.place ?? connections[0]?.place ?? 1)
  const conn = connections.find((c) => c.place === place) ?? null
  const fields = conn?.fields ?? []
  const [drafts, setDrafts] = useState<Record<string, Draft>>(() => draftsFor(fields, target ? targetValues(target) : {}))
  const [retries, setRetries] = useState(target?.retries ?? true)
  const [policy, setPolicy] = useState<string>(target?.policy ?? "on-demand")
  // The session whose minion carries its calls and opens; none: an
  // instance of the channel.
  const [delegate, setDelegate] = useState<DelegateSession | null>(target?.delegate ?? null)
  const [submitted, setSubmitted] = useState(false)
  // A target that needs editing opens with what no longer fits, on its fields.
  const [serverProblems, setServerProblems] = useState<Record<string, string>>(() =>
    Object.fromEntries((target?.problems ?? []).map((p) => [p.key ?? "", p.message])),
  )
  // What no field of the form shows: a field the channel dropped, the
  // target as a whole. Saving the form leaves a dropped field out.
  const [error, setError] = useState<string | null>(() => {
    const unplaced = (target?.problems ?? []).filter((p) => !p.key || !fields.some((f) => f.key === p.key))
    return unplaced.length > 0
      ? unplaced.map((p) => (p.key ? `${p.key}: ${p.message} (saving drops it)` : p.message)).join("; ")
      : null
  })
  const pending = add.isPending || update.isPending

  const { values, errors } = evaluateParams(fields, drafts)
  // A minion core keeps a connection to (bind-persistent): who opens it.
  const persistent = conn?.kind === "bind-persistent"
  const errorOf = (key: string) => (submitted ? errors[key] : undefined) ?? serverProblems[key]
  // Only delegates carry a delegated-only kind; an agentless kind's channel
  // is the minion, so it has none.
  const delegateMissing = !!conn?.delegatedOnly && !delegate
  const delegateError = (submitted && delegateMissing ? "Only delegates carry this kind: pick one." : undefined) ?? serverProblems.delegate

  function pick(next: number) {
    setPlace(next)
    // Another connection may be of another channel: its own fields.
    const c = connections.find((x) => x.place === next)
    setDrafts(draftsFor(c?.fields ?? [], {}))
    setServerProblems({})
  }

  async function save() {
    setSubmitted(true)
    setError(null)
    if (!conn || conn.problem || Object.keys(errors).length > 0 || delegateMissing || pending) return
    const delegateId = conn.agentless ? null : (delegate?.id ?? null)
    try {
      if (target) {
        await update.mutateAsync({ id: target.id, input: { values, retries, policy: persistent ? policy : null, delegateId } })
        toast.success("Target saved", { description: target.state === "connected" ? undefined : "Core calls it again now." })
      } else {
        await add.mutateAsync({
          buildId,
          conn: conn.place,
          input: { values, retries, policy: persistent ? policy : null, delegateId },
          sessionId: session?.id ?? null,
        })
        toast.success("Target added", {
          description: session
            ? `Core calls it now, under session #${session.number}: once the minion answers, the session goes on.`
            : "Core calls it now: once the minion answers, its session opens.",
        })
      }
      onClose()
    } catch (err) {
      const problems = problemsFromError(err)
      setServerProblems(problems)
      const unplaced = Object.entries(problems).filter(
        ([key]) => key === "" || (key !== "delegate" && !fields.some((f) => f.key === key)),
      )
      setError(
        unplaced.length > 0
          ? unplaced.map(([key, message]) => (key ? `${key}: ${message}` : message)).join("; ")
          : Object.keys(problems).length > 0
            ? null
            : errorMessage(err, target ? "Failed to save the target" : "Failed to add the target"),
      )
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>
          {target ? `Edit target ${target.name}` : session ? `Add a target for session #${session.number}` : "Add a target"}
        </DialogTitle>
        <DialogDescription>
          {session
            ? `Where the minion is now: core calls it under session #${session.number}, and the session goes on with its history. It must be this build's: it keeps nothing, so wherever it is, it answers under the session's id.`
            : "Where the minion is: core calls it through the connection's channel, and the minion's hello opens its session."}{" "}
          Nothing here goes into the build: only the channel reads it.
        </DialogDescription>
      </DialogHeader>

      <div className="@container/settings flex max-h-[60vh] flex-col gap-4 overflow-y-auto pr-1">
        {connections.length > 1 && !target && (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="target-conn">Connection</Label>
            <Select value={String(place)} onValueChange={(v) => v && pick(Number(v))} disabled={pending}>
              <SelectTrigger id="target-conn" className="w-full">
                <SelectValue>{conn ? `${conn.place}. ${conn.name}` : null}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {connections.map((c) => (
                  <SelectItem key={c.place} value={String(c.place)} disabled={!!c.problem}>
                    {c.place}. {c.name} · {c.moduleName} {c.problem ? `(${c.problem})` : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        {conn && (
          <p className="text-sm text-muted-foreground">
            Connection {conn.place}, <span className="font-medium text-foreground">{conn.name}</span>, through the{" "}
            <span className="font-mono">{conn.moduleName}</span> channel (<span className="font-mono">{conn.kind}</span>).
          </p>
        )}
        {conn?.problem && (
          <div className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">{conn.problem}</div>
        )}

        {groupByField(fields, (f) => f).map((g) => (
          <SettingsSection key={g.group} title={g.group}>
            {g.items.map((f) => {
              const id = `target-${f.key}`
              const err = errorOf(f.key)
              return (
                <SettingRow
                  key={f.key}
                  id={id}
                  label={f.label}
                  fieldKey={f.key}
                  required={f.required}
                  help={f.description ?? undefined}
                  error={err}
                >
                  <FieldControl
                    field={f}
                    id={id}
                    draft={drafts[f.key] ?? initialDraft(f, null, { secretAsText: true })}
                    onChange={(d) => {
                      setDrafts((prev) => ({ ...prev, [f.key]: d }))
                      setServerProblems((prev) => {
                        const next = { ...prev }
                        delete next[f.key]
                        return next
                      })
                    }}
                    disabled={pending}
                    invalid={!!err}
                    defaultValue={f.default}
                  />
                </SettingRow>
              )
            })}
          </SettingsSection>
        ))}

        <SettingsSection title={persistent ? "Connection" : "Calls"}>
          {conn && !conn.agentless && (
            <SettingRow
              id="target-delegate"
              label="Through a delegate"
              required={conn.delegatedOnly}
              error={delegateError}
              help={
                conn.delegatedOnly
                  ? `Only delegates carry ${conn.moduleName} ${conn.kind} connections: a session whose minion serves them on its host, while a delegate task of its runs (core starts one when none does).`
                  : `A session whose minion serves ${conn.moduleName} ${conn.kind} connections on its host carries this target's ${persistent ? "opens" : "calls"}, while a delegate task of its runs (core starts one when none does). None: an instance of the channel does.`
              }
            >
              <DelegatePicker
                id="target-delegate"
                operationId={operationId}
                moduleName={conn.moduleName}
                kind={conn.kind}
                version={conn.version}
                value={delegate}
                onChange={(s) => {
                  setDelegate(s)
                  setServerProblems((prev) => {
                    const next = { ...prev }
                    delete next.delegate
                    return next
                  })
                }}
                required={conn.delegatedOnly}
                disabled={pending}
                invalid={!!delegateError}
              />
            </SettingRow>
          )}
          {persistent && (
            <SettingRow id="target-policy" label="Opened by" help={TARGET_POLICY_HELP[policy]}>
              <Select value={policy} onValueChange={(v) => v && setPolicy(v)} disabled={pending}>
                <SelectTrigger id="target-policy" className="w-full">
                  <SelectValue>{TARGET_POLICY_LABEL[policy] ?? policy}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {TARGET_POLICIES.map((p) => (
                    <SelectItem key={p} value={p}>
                      {TARGET_POLICY_LABEL[p]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </SettingRow>
          )}
          <SettingRow
            id="target-retries"
            label="Retry when unreachable"
            help="Core calls an unreachable target again after 1 minute, 5, 30, then hourly, for a day. Off where every request in the server's log counts: then only Check now calls it again."
          >
            <Switch id="target-retries" checked={retries} onCheckedChange={setRetries} disabled={pending} />
          </SettingRow>
        </SettingsSection>
      </div>

      {error && <p className="text-sm text-destructive">{error}</p>}
      <DialogFooter>
        <Button variant="outline" onClick={onClose} disabled={pending}>
          Cancel
        </Button>
        <Button onClick={save} disabled={pending || !conn || !!conn.problem}>
          {pending
            ? "Saving..."
            : persistent
              ? target
                ? "Save"
                : policy === "manual"
                  ? "Add"
                  : "Add and connect"
              : target
                ? "Save and call"
                : "Add and call"}
        </Button>
      </DialogFooter>
    </>
  )
}

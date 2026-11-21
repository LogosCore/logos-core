import { useMemo } from "react"
import { CardColumns } from "@/components/ui/card-columns"
import { IdRows, InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useRecordModals } from "@/hooks/use-record-modals"
import { isRecord } from "@/lib/json-edit"
import { compareFormOrder } from "@/components/modules/management/field-values"
import { valueRows, type ValueRow } from "@/components/builds/build-record"
import { rowsOf } from "@/components/builds/build-value-rows"
import type { ConsoleCommand } from "./command-bar"
import { paramApplies } from "./command-line"

type Delegate = NonNullable<MinionTaskFieldsFragment["delegate"]>

const NO_FILES: ReadonlyMap<string, string> = new Map()

/**
 * A delegate task's view (docs: contracts/minion-messages/delegation): the
 * params it runs with (a reverse delegate listens as they say: they are its
 * command's own, so the factory's labels name them), the channel kinds its
 * minion serves for other minions, and what goes through it now, held by
 * core in memory as a tunnel's streams are.
 */
export function DelegateView({ delegate: d, args, command }: { delegate: Delegate; args: unknown; command?: ConsoleCommand }) {
  const modals = useRecordModals()
  const params = useMemo(() => paramRows(command, args), [command, args])
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      <CardColumns>
        {params.length > 0 && (
          <InfoCard title="Params" note={params.length}>
            <IdRows rows={rowsOf(params, NO_FILES)} />
          </InfoCard>
        )}
        <InfoCard title="Serves" note={d.serves.length}>
          <IdRows
            rows={d.serves.map((s) => ({
              id: s.moduleName,
              content: (
                <span>
                  <span className="font-mono">{s.kinds.join(", ")}</span>
                  <span className="text-muted-foreground"> · schema {s.versions.join(", ")}</span>
                </span>
              ),
            }))}
          />
        </InfoCard>
        <InfoCard title="Through it">
          {!d.held ? (
            <NoteRow>Nothing has gone through it since core started.</NoteRow>
          ) : (
            <>
              <LabelRow label="Carried">{d.carried}</LabelRow>
              <LabelRow label="Waiting for its minion">{d.waiting}</LabelRow>
              {d.usedAt && (
                <LabelRow label="Last used">
                  <FormattedDateTimeText date={d.usedAt} />
                </LabelRow>
              )}
              <LabelRow label="Stops when idle for">{d.stopIdle ?? "never"}</LabelRow>
            </>
          )}
        </InfoCard>
        <InfoCard title="Connections" note={d.connections.length}>
          {d.connections.length === 0 ? (
            <NoteRow>No connection is open through it.</NoteRow>
          ) : (
            <IdRows
              rows={d.connections.map((c) => ({
                id: String(c.n),
                content: (
                  <div className="flex items-baseline gap-2">
                    <div className="min-w-0 flex-1">
                      <span className="font-mono">{c.moduleName}</span>
                      <span className="text-muted-foreground"> · {c.kind}</span>
                      {c.peer && <span className="text-muted-foreground"> · {c.peer}</span>}
                      <div className="text-xs text-muted-foreground">
                        opened <FormattedDateTimeText date={c.openedAt} />
                        {c.session ? ` · session #${c.session.number}` : " · its minion has not spoken yet"}
                      </div>
                    </div>
                    {c.session && (
                      <button
                        type="button"
                        className="shrink-0 text-xs underline-offset-2 hover:underline"
                        onClick={() => c.session && modals.open({ kind: "session", id: c.session.id })}
                      >
                        #{c.session.number}
                      </button>
                    )}
                  </div>
                ),
              }))}
            />
          )}
        </InfoCard>
      </CardColumns>
    </div>
  )
}

/**
 * A task's params as its minion got them: each value given over its
 * param's default (protocol TaskArgs), dimmed where it is the default; the
 * params that apply, in the form's order. None without its command.
 */
function paramRows(command: ConsoleCommand | undefined, args: unknown): ValueRow[] {
  if (!command) return []
  const values: Record<string, unknown> = {}
  for (const f of command.fields) if (f.default !== null && f.default !== undefined) values[f.key] = f.default
  if (isRecord(args)) Object.assign(values, args)
  const applies = new Set(command.params.filter((p) => paramApplies(p, command.params, values)).map((p) => p.key))
  return valueRows([...command.fields].filter((f) => applies.has(f.key)).sort(compareFormOrder), values)
}

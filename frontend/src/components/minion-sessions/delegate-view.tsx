import { CardColumns } from "@/components/ui/card-columns"
import { IdRows, InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useRecordModals } from "@/hooks/use-record-modals"

type Delegate = NonNullable<MinionTaskFieldsFragment["delegate"]>

/**
 * A delegate task's view (docs: contracts/minion-messages/delegation): the
 * channel kinds its minion serves for other minions, and what goes through
 * it now, held by core in memory as a tunnel's streams are.
 */
export function DelegateView({ delegate: d }: { delegate: Delegate }) {
  const modals = useRecordModals()
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      <CardColumns>
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

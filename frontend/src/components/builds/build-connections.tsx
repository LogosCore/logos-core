import { useMemo, useState } from "react"
import { CardColumns } from "@/components/ui/card-columns"
import { IdRows, InfoCard, NoteRow, SectionRow } from "@/components/ui/info-card"
import { useBuildFiles, useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { factoryKindsByName, paramFields, recordedConnections, settingFields, settingsSummary } from "@/components/builds/build-values"
import { valueRows, type BuildRecord, type ValueRow } from "@/components/builds/build-record"
import { failoverAuto, failoverSummary, recordFailover } from "@/components/builds/failover-values"
import { rowsOf } from "@/components/builds/build-value-rows"
import { ConnectionFoldCard } from "./connection-fold-card"

/**
 * The build modal's Connections tab: how the minion reaches its channels, in
 * the order it tries them, a card each that folds (the first, the one it
 * tries first, starts unfolded): its head says what it is, where it
 * connects, the team's note and its own failover settings; unfolded, its
 * channel's settings and the factory's values for it: those set, values at
 * their default dimmed, the rest left out. The connections on the left;
 * failover, where there are two or more, on the right.
 */
export function BuildConnections({ build: b }: { build: BuildRecord }) {
  const kinds = useFactoryKinds()
  const channelKinds = useChannelKinds()
  const files = useBuildFiles(b.operationId)
  const channelIcons = useChannelIcons()
  const kind = useMemo(() => factoryKindsByName(kinds.data?.factoryKinds ?? []).get(b.moduleName), [kinds.data, b.moduleName])
  const channels = useMemo(() => kindsByName(channelKinds.data?.channelKinds ?? []), [channelKinds.data])
  const fileNames = useMemo(
    () => new Map((files.data?.buildFiles ?? []).map((f) => [f.id, f.name])),
    [files.data],
  )
  const connections = recordedConnections(b)
  const failover = recordFailover(b.failover)
  // The cards unfolded, by place: the first to start with.
  const [unfolded, setUnfolded] = useState<ReadonlySet<number>>(() => new Set([1]))
  const toggle = (place: number) =>
    setUnfolded((was) => {
      const next = new Set(was)
      if (next.has(place)) next.delete(place)
      else next.add(place)
      return next
    })

  return (
    <div className="min-w-0 px-4 py-6">
      <CardColumns>
        {/* The connections on the left, failover on the right. */}
        <div className="flex min-w-0 flex-col gap-4">
          {connections.map((c, i) => {
            const place = i + 1
            const settings = valueRows(settingFields(channels.get(c.moduleName), c.kind), c.params).filter(isSet)
            const own = valueRows(kind ? paramFields(kind, c.moduleName, c.kind) : [], c.values).filter(isSet)
            // Headed only when the factory has values for the channel too.
            const parts = own.length > 0
            const address = settingsSummary(channels.get(c.moduleName), c.kind, c.params)
            const ownFailover =
              connections.length > 1 && (!failoverAuto(c.kind, c.failover) || c.failover?.fail_after !== undefined)
                ? [
                    !failoverAuto(c.kind, c.failover) && "Failover does not move the minion here (a switch does)",
                    c.failover?.fail_after !== undefined && `moves off after ${c.failover.fail_after} failures`,
                  ]
                    .filter(Boolean)
                    .join(" · ")
                : null
            return (
              <ConnectionFoldCard
                key={i}
                place={place}
                name={c.name}
                moduleName={c.moduleName}
                kind={c.kind}
                icon={channelIcons.get(c.moduleName)}
                expanded={unfolded.has(place)}
                onToggle={() => toggle(place)}
                lines={
                  <>
                    {address && (
                      <div className="truncate font-mono text-xs text-muted-foreground" title={address}>
                        {c.moduleName} · {address}
                      </div>
                    )}
                    {c.description && <p className="text-xs">{c.description}</p>}
                    {ownFailover && <p className="text-xs text-muted-foreground">{ownFailover}</p>}
                  </>
                }
              >
                {parts && <SectionRow title="Settings" note={c.moduleName} />}
                {settings.length === 0 ? <NoteRow>No settings set.</NoteRow> : <IdRows rows={rowsOf(settings, fileNames)} />}
                {parts && (
                  <>
                    <SectionRow title={`${b.moduleName}'s values`} note={`for ${c.moduleName}`} />
                    <IdRows rows={rowsOf(own, fileNames)} />
                  </>
                )}
              </ConnectionFoldCard>
            )
          })}
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          {connections.length > 1 && (
            <InfoCard title="Failover" note={failover.enabled ? "on" : "off"}>
              <NoteRow>
                {failoverSummary(failover)}
                {kind && !kind.failover && `. ${b.moduleName} does not say its minions apply these.`}
              </NoteRow>
              <IdRows
                rows={[
                  { id: "enabled", content: failover.enabled ? "yes" : "no" },
                  { id: "fail_after", content: `${failover.fail_after} failures in a row` },
                  { id: "fail_for", content: failover.fail_for },
                  { id: "wrap", content: failover.wrap ? "yes" : "no" },
                  { id: "cycle_pause", content: failover.cycle_pause },
                  { id: "failback_after", content: failover.failback_after },
                ]}
              />
            </InfoCard>
          )}
        </div>
      </CardColumns>
    </div>
  )
}

/** A row with a value: one not set says nothing about the connection. */
function isSet(r: ValueRow): boolean {
  return r.value !== undefined
}

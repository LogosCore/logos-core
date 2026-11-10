import { useMemo } from "react"
import { CardColumns } from "@/components/ui/card-columns"
import { IdRows, InfoCard, NoteRow, SectionRow } from "@/components/ui/info-card"
import { useBuildFiles, useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { KIND_FLOWS } from "@/components/endpoints/connection-kind-flows"
import { ModuleIcon } from "@/components/modules/module-icon"
import { factoryKindsByName, paramFields, recordedConnections, settingFields } from "@/components/builds/build-values"
import { valueRows, type BuildRecord, type ValueRow } from "@/components/builds/build-record"
import { failoverAuto, failoverSummary, recordFailover } from "@/components/builds/failover-values"
import { rowsOf } from "@/components/builds/build-value-rows"

/**
 * The build modal's Connections tab: how the minion reaches its channels, in
 * the order it tries them (the tab says how many), each with its channel's
 * settings and the factory's values for it: those set, values at their
 * default dimmed, the rest left out. Then failover, where there are two or
 * more. Cards as the Overview lays them out, two side by side.
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

  return (
    <div className="min-w-0 px-4 py-6">
      <CardColumns>
        {connections.map((c, i) => {
          const settings = valueRows(settingFields(channels.get(c.moduleName), c.kind), c.params).filter(isSet)
          const own = valueRows(kind ? paramFields(kind, c.moduleName, c.kind) : [], c.values).filter(isSet)
          // Headed only when the factory has values for the channel too.
          const parts = own.length > 0
          return (
            <InfoCard
              key={i}
              title={
                <span className="flex min-w-0 items-baseline gap-2">
                  <ModuleIcon type="channel" icon={channelIcons.get(c.moduleName)} className="size-4 self-center" />
                  <span className="truncate font-mono">{c.name}</span>
                </span>
              }
              note={
                <>
                  {c.moduleName} · {KIND_FLOWS[c.kind]?.label ?? c.kind}
                  {KIND_FLOWS[c.kind] && <span className="font-mono"> ({c.kind})</span>} · connection {i + 1} of{" "}
                  {connections.length}
                </>
              }
            >
              {c.description && <NoteRow>{c.description}</NoteRow>}
              {connections.length > 1 && (!failoverAuto(c.kind, c.failover) || c.failover?.fail_after !== undefined) && (
                <NoteRow>
                  {[
                    !failoverAuto(c.kind, c.failover) && "Failover does not move the minion here (a switch does)",
                    c.failover?.fail_after !== undefined && `moves off after ${c.failover.fail_after} failures`,
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </NoteRow>
              )}
              {parts && <SectionRow title="Settings" note={c.moduleName} />}
              {settings.length === 0 ? <NoteRow>No settings set.</NoteRow> : <IdRows rows={rowsOf(settings, fileNames)} />}
              {parts && (
                <>
                  <SectionRow title={`${b.moduleName}'s values`} note={`for ${c.moduleName}`} />
                  <IdRows rows={rowsOf(own, fileNames)} />
                </>
              )}
            </InfoCard>
          )
        })}

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
      </CardColumns>
    </div>
  )
}

/** A row with a value: one not set says nothing about the connection. */
function isSet(r: ValueRow): boolean {
  return r.value !== undefined
}

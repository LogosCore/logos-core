import { useMemo } from "react"
import { IdRows, InfoCard, NoteRow, SectionRow } from "@/components/ui/info-card"
import { useBuildFiles, useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { KIND_FLOWS } from "@/components/endpoints/connection-kind-flows"
import { ModuleIcon } from "@/components/modules/module-icon"
import { factoryKindsByName, paramFields, settingFields } from "@/components/builds/build-values"
import { valueRows, type ValueRow } from "@/components/builds/build-record"
import { rowsOf } from "@/components/builds/build-value-rows"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"

/**
 * A card per connection of the session's minion, as the build modal's
 * Connections tab shows a build's: its channel's settings and the factory's
 * values for it, those set, values at their default dimmed. The connections
 * are the minion's own when its hello said them (values then by number),
 * else its build's.
 */
export function ConnectionDetailCards({ s }: { s: MinionSessionFieldsFragment }) {
  const { data } = useMinionSessionConnections(s.id)
  const connections = data?.minionSession?.connections ?? []
  const kinds = useFactoryKinds()
  const channelKinds = useChannelKinds()
  const files = useBuildFiles(s.operationId)
  const channelIcons = useChannelIcons()
  const kind = useMemo(() => factoryKindsByName(kinds.data?.factoryKinds ?? []).get(s.moduleName), [kinds.data, s.moduleName])
  const channels = useMemo(() => kindsByName(channelKinds.data?.channelKinds ?? []), [channelKinds.data])
  const fileNames = useMemo(() => new Map((files.data?.buildFiles ?? []).map((f) => [f.id, f.name])), [files.data])

  return (
    <>
      {connections.map((c) => {
        const settings = valueRows(settingFields(channels.get(c.moduleName), c.kind), c.params).filter(isSet)
        const own = valueRows(kind ? paramFields(kind, c.moduleName, c.kind) : [], c.values).filter(isSet)
        const parts = own.length > 0
        return (
          <InfoCard
            key={c.place}
            title={
              <span className="flex min-w-0 items-baseline gap-2">
                <ModuleIcon type="channel" icon={channelIcons.get(c.moduleName)} className="size-4 self-center" />
                <span className="truncate font-mono">{c.name}</span>
              </span>
            }
            note={
              <>
                {c.moduleName} · {KIND_FLOWS[c.kind]?.label ?? c.kind}
                {KIND_FLOWS[c.kind] && <span className="font-mono"> ({c.kind})</span>} · connection {c.place} of{" "}
                {connections.length}
                {c.place === s.conn && " · in use"}
              </>
            }
          >
            {parts && <SectionRow title="Settings" note={c.moduleName} />}
            {settings.length === 0 ? <NoteRow>No settings set.</NoteRow> : <IdRows rows={rowsOf(settings, fileNames)} />}
            {parts && (
              <>
                <SectionRow title={`${s.moduleName}'s values`} note={`for ${c.moduleName}`} />
                <IdRows rows={rowsOf(own, fileNames)} />
              </>
            )}
          </InfoCard>
        )
      })}
    </>
  )
}

/** A row with a value: one not set says nothing about the connection. */
function isSet(r: ValueRow): boolean {
  return r.value !== undefined
}

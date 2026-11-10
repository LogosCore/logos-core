import { useMemo } from "react"
import { IdRows, InfoCard, NoteRow, type IdRow } from "@/components/ui/info-card"
import { useFactoryKinds } from "@/graphql/hooks/builds"
import { factoryKindsByName, type Command } from "@/components/builds/build-values"
import type { BuildRecord } from "@/components/builds/build-record"

/**
 * The build modal's Commands tab: what the minion can do and nothing else,
 * in the factory's order (the tab says how many). A list read down, as wide
 * as an Overview card, not across the modal.
 */
export function BuildCommands({ build: b }: { build: BuildRecord }) {
  const kinds = useFactoryKinds()
  const kind = useMemo(() => factoryKindsByName(kinds.data?.factoryKinds ?? []).get(b.moduleName), [kinds.data, b.moduleName])
  return (
    <div className="min-w-0 px-4 py-6">
      <div className="max-w-4xl">
        <InfoCard>
          {b.commands.length === 0 ? (
            <NoteRow>None: the minion was built without commands.</NoteRow>
          ) : (
            <IdRows rows={commandRows(b.commands, kind?.commands ?? [])} />
          )}
        </InfoCard>
      </div>
    </div>
  )
}

/**
 * The build's commands in the factory's order: each id, and its label and
 * description by today's schema; one the factory no longer has says so.
 */
function commandRows(ids: readonly string[], known: readonly Command[]): IdRow[] {
  const byId = new Map(known.map((c) => [c.id, c]))
  return ids.map((id) => {
    const c = byId.get(id)
    return {
      id,
      content: c ? (
        <>
          <div>
            {c.label}
            {c.required && <span className="text-xs text-muted-foreground"> · always built in</span>}
          </div>
          {c.description && <div className="text-xs text-muted-foreground">{c.description}</div>}
        </>
      ) : (
        <div className="text-muted-foreground">No longer a command of the factory</div>
      ),
    }
  })
}

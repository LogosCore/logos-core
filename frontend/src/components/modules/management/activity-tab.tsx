import { useMemo, useState } from "react"
import { PlayIcon, SlidersHorizontalIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import {
  useModuleActionInvocations,
  useModuleConfigChanges,
} from "@/graphql/hooks/modules"
import { InvocationDetails, InvocationStatusBadge } from "./actions-tab"
import { durationMs, formatElapsed, formatValue, mergeActivity } from "./field-values"
import { actorLabel, scopeLabel } from "./labels"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>

export function ActivityTab({
  module,
  management,
}: {
  module: ModuleDetailFieldsFragment
  management: Management
}) {
  const changes = useModuleConfigChanges(module.instance)
  const invocations = useModuleActionInvocations(module.instance)
  const [expanded, setExpanded] = useState<string | null>(null)

  const items = useMemo(
    () =>
      mergeActivity(
        changes.data?.moduleConfigChanges ?? [],
        invocations.data?.moduleActionInvocations ?? [],
      ),
    [changes.data, invocations.data],
  )

  // Field metadata for formatting old/new values; unknown keys (a property the
  // manifest no longer declares) fall back to raw JSON.
  const fieldTypes = useMemo(() => {
    const m = new Map<string, { type: string; label: string; unit?: string | null }>()
    for (const p of management.properties) m.set(p.field.key, p.field)
    return m
  }, [management.properties])
  const actionLabels = useMemo(
    () => new Map(management.actions.map((a) => [a.name, a.label])),
    [management.actions],
  )

  if (changes.isLoading || invocations.isLoading) {
    return <p className="py-8 text-center text-sm text-muted-foreground">Loading…</p>
  }
  if (items.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        No configuration changes or actions yet.
      </p>
    )
  }

  return (
    <ol className="flex flex-col divide-y rounded-xl border">
      {items.map((item) => {
        if (item.kind === "config") {
          const c = item.change
          return (
            <li key={`c-${c.id}`} className="flex gap-3 px-4 py-3">
              <SlidersHorizontalIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-medium">{actorLabel(c.actor)}</span>
                  <span className="text-muted-foreground">changed configuration for</span>
                  <Badge variant="secondary">
                    {c.scope === "MODULE"
                      ? scopeLabel("MODULE", c.moduleName)
                      : c.instance === module.instance
                        ? "This instance"
                        : c.instance}
                  </Badge>
                  <span className="text-xs text-muted-foreground">rev {c.revision}</span>
                  <FormattedDateTimeText
                    date={c.at}
                    format="yyyy.mm.dd hh:MM:ss"
                    className="ml-auto text-xs text-muted-foreground"
                  />
                </div>
                <ul className="flex flex-col gap-0.5 font-mono text-xs">
                  {c.changes.map((k) => {
                    const f = fieldTypes.get(k.key)
                    const fmt = (v: unknown) =>
                      f ? formatValue(f.type, v, f.unit) : v === null || v === undefined ? "—" : JSON.stringify(v)
                    return (
                      <li key={k.key} className="flex flex-wrap gap-1.5">
                        <span className="text-foreground">{f?.label ?? k.key}:</span>
                        <span className="break-all text-muted-foreground">{fmt(k.old)}</span>
                        <span aria-hidden>→</span>
                        <span className="break-all">{fmt(k.new)}</span>
                      </li>
                    )
                  })}
                </ul>
              </div>
            </li>
          )
        }
        const inv = item.invocation
        const ms = durationMs(inv.startedAt, inv.finishedAt)
        const open = expanded === inv.id
        return (
          <li key={`a-${inv.id}`} className="flex gap-3 px-4 py-3">
            <PlayIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <span className="font-medium">{actorLabel(inv.actor)}</span>
                <span className="text-muted-foreground">ran</span>
                <span className="font-medium">{actionLabels.get(inv.action) ?? inv.action}</span>
                <InvocationStatusBadge status={inv.status} />
                {ms !== null && (
                  <span className="text-xs text-muted-foreground">{formatElapsed(ms)}</span>
                )}
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => setExpanded(open ? null : inv.id)}
                  aria-expanded={open}
                >
                  {open ? "Hide details" : "Details"}
                </Button>
                <FormattedDateTimeText
                  date={inv.startedAt}
                  format="yyyy.mm.dd hh:MM:ss"
                  className="ml-auto text-xs text-muted-foreground"
                />
              </div>
              {open && <InvocationDetails invocation={inv} />}
            </div>
          </li>
        )
      })}
    </ol>
  )
}

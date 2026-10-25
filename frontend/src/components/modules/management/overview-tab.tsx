import type { ReactNode } from "react"
import { Badge } from "@/components/ui/badge"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { ModuleHealthIndicator } from "@/components/modules/module-health-indicator"
import { cn } from "@/lib/utils"
import { formatValue } from "./field-values"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[10rem_1fr] gap-3 px-4 py-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  )
}

function DateOrDash({ date }: { date?: string | null }) {
  return date ? <FormattedDateTimeText date={date} format="yyyy.mm.dd hh:MM:ss" /> : <>—</>
}

export function ModuleIdentity({ module }: { module: ModuleDetailFieldsFragment }) {
  const m = module
  return (
    <section className="rounded-xl border bg-card">
      <h3 className="border-b px-4 py-2 text-sm font-semibold">Identity</h3>
      <dl className="divide-y">
        <Row label="Instance">
          <code className="font-mono">{m.instance}</code>
        </Row>
        <Row label="Module">{m.name || "—"}</Row>
        <Row label="Type">{m.type}</Row>
        <Row label="Version">{m.version || "—"}</Row>
        <Row label="Description">{m.description || "—"}</Row>
        <Row label="Status">
          <ModuleStatusIndicator status={m.status} lastHeartbeatAt={m.lastHeartbeatAt} />
        </Row>
        <Row label="Health">
          <ModuleHealthIndicator health={m.lastStatus} registered={m.status === "registered"} />
        </Row>
        <Row label="Registered">
          <DateOrDash date={m.registeredAt} />
        </Row>
        <Row label="Last heartbeat">
          <DateOrDash date={m.lastHeartbeatAt} />
        </Row>
        {m.deregisteredAt && (
          <Row label="Deregistered">
            <DateOrDash date={m.deregisteredAt} />
            {m.deregisterReason && (
              <span className="text-muted-foreground"> — {m.deregisterReason}</span>
            )}
          </Row>
        )}
        {m.declaredDeadAt && (
          <Row label="Declared dead">
            <DateOrDash date={m.declaredDeadAt} />
          </Row>
        )}
        {m.management && (
          <>
            <Row label="Manifest schema">{m.management.schemaVersion}</Row>
            <Row label="Config revision">
              {m.management.appliedRevision} applied / {m.management.revision} desired
            </Row>
          </>
        )}
      </dl>
    </section>
  )
}

export function StatusValues({ management }: { management: Management }) {
  return (
    <section className="rounded-xl border bg-card">
      <h3 className="border-b px-4 py-2 text-sm font-semibold">Status</h3>
      {management.status.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted-foreground">
          This module reports no status values.
        </p>
      ) : (
        <dl className="divide-y">
          {management.status.map((s) => (
            <div key={s.key} className="grid grid-cols-[10rem_1fr] gap-3 px-4 py-2 text-sm">
              <dt className="text-muted-foreground" title={s.description ?? undefined}>
                {s.label}
              </dt>
              <dd className="min-w-0 break-words">
                <StatusValue status={s} />
                {s.description && (
                  <div className="text-xs text-muted-foreground">{s.description}</div>
                )}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

type StatusEntry = Management["status"][number]

// The module declares which value needs attention (`alert`); core never
// guesses from the key or the value. With an alert declared, a matching value
// is amber and any other green. Without one, values stay neutral.
const TONE = {
  alert: "border-amber-500/50 bg-amber-500/15 text-amber-700 dark:text-amber-400",
  ok: "border-green-600/40 bg-green-600/10 text-green-700 dark:text-green-400",
}

function StatusValue({ status: s }: { status: StatusEntry }) {
  const declared = s.alert !== null && s.alert !== undefined
  const reported = s.value !== null && s.value !== undefined
  const tone = declared && reported ? (s.value === s.alert ? "alert" : "ok") : null
  if (s.type === "bool" && typeof s.value === "boolean") {
    return (
      <Badge variant={s.value ? "default" : "outline"} className={cn(tone && TONE[tone])}>
        {s.value ? "Yes" : "No"}
      </Badge>
    )
  }
  const text = formatValue(s.type, s.value, s.unit)
  if (tone) {
    return (
      <Badge variant="outline" className={TONE[tone]}>
        {text}
      </Badge>
    )
  }
  return <span className="tabular-nums">{text}</span>
}

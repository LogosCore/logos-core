import type { ReactNode } from "react"
import { Badge } from "@/components/ui/badge"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { TableBody, TableCell, TableRow } from "@/components/ui/table"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { ModuleHealthIndicator } from "@/components/modules/module-health-indicator"
import { cn } from "@/lib/utils"
import { platformLabel } from "@/components/builds/build-values"
import { compareFields, formatValue, type FieldDef } from "./field-values"
import { channelsText, fieldNotes, versionsText } from "./schema-notes"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>
type Connection = NonNullable<ModuleDetailFieldsFragment["connection"]>
type Build = NonNullable<ModuleDetailFieldsFragment["build"]>

/**
 * The minion connection settings this instance declared (read-only). Operators
 * fill them in for each connection of a build, or keep them as endpoints on an
 * operation's Endpoints page.
 */
export function ConnectionSchema({ connection }: { connection: Connection }) {
  return (
    <section className="rounded-xl border bg-card">
      <h3 className="flex items-baseline gap-2 border-b px-4 py-2 text-sm font-semibold">
        Minion connection settings
        <span className="text-xs font-normal text-muted-foreground">schema v{connection.version}</span>
      </h3>
      <dl className="divide-y">
        <FieldRows fields={connection.fields} />
      </dl>
    </section>
  )
}

/**
 * The build parameters this factory instance declared (read-only): the
 * parameters every target takes, each target's own, and the connection
 * parameters per channel kind. Operators fill them in on an operation's
 * Builds page.
 */
export function BuildParameters({ build }: { build: Build }) {
  return (
    <section className="rounded-xl border bg-card">
      <h3 className="flex items-baseline gap-2 border-b px-4 py-2 text-sm font-semibold">
        Build parameters
        <span className="text-xs font-normal text-muted-foreground">schema v{build.version}</span>
      </h3>
      <div className="divide-y">
        {build.fields.length > 0 && (
          <SchemaGroup title="Parameters of every target">
            <FieldRows fields={build.fields} />
          </SchemaGroup>
        )}
        {build.targets
          .filter((t) => t.fields.length > 0)
          .map((t) => (
            <SchemaGroup key={t.id} title={`${t.label} parameters`}>
              <FieldRows fields={t.fields} />
            </SchemaGroup>
          ))}
        {build.channels.map((c) => (
          <SchemaGroup
            key={c.moduleName}
            title={`${c.moduleName} connection parameters`}
            note={`connection schema ${versionsText(c.versions)}`}
          >
            {c.fields.length > 0 ? (
              <FieldRows fields={c.fields} />
            ) : (
              <div className="px-4 py-2 text-sm text-muted-foreground">None: the connection settings are used as they are.</div>
            )}
          </SchemaGroup>
        ))}
        <dl>
          <div className="grid grid-cols-[10rem_1fr] gap-3 px-4 py-2 text-sm">
            <dt className="text-muted-foreground">Payload crypto</dt>
            <dd className="min-w-0 break-words">{build.crypto.length > 0 ? build.crypto.join(", ") : "None"}</dd>
          </div>
        </dl>
      </div>
    </section>
  )
}

/**
 * The targets this factory instance declared (read-only), what it can
 * produce: each one's id, and its label, description, format, platforms and
 * the channels it speaks.
 */
export function BuildTargets({ build }: { build: Build }) {
  return (
    <IdTable
      title="Targets"
      rows={build.targets.map((t) => ({
        id: t.id,
        content: (
          <>
            <div>{t.label}</div>
            {t.description && <div className="text-xs text-muted-foreground">{t.description}</div>}
            <div className="text-xs text-muted-foreground">
              {[t.format, t.platforms.map(platformLabel).join(", "), t.channels.length > 0 ? `speaks ${channelsText(t.channels)}` : ""]
                .filter(Boolean)
                .join(" · ")}
            </div>
          </>
        ),
      }))}
    />
  )
}

/**
 * The commands this factory instance declared (read-only), what its minions
 * can be built with: each one's id, and its label and description, in the
 * factory's order. Operators pick a build's in the build form.
 */
export function BuildCommands({ build }: { build: Build }) {
  return (
    <IdTable
      title="Commands"
      rows={build.commands.map((c) => ({
        id: c.id,
        content: (
          <>
            <div>{c.label}</div>
            {c.description && <div className="text-xs text-muted-foreground">{c.description}</div>}
          </>
        ),
      }))}
    />
  )
}

/** A card titled with its count, holding a table without headings: each row an id, and what is said of it. */
function IdTable({ title, rows }: { title: string; rows: { id: string; content: ReactNode }[] }) {
  return (
    <section className="rounded-xl border bg-card">
      <h3 className="flex items-baseline gap-2 border-b px-4 py-2 text-sm font-semibold">
        {title}
        <span className="text-xs font-normal text-muted-foreground tabular-nums">{rows.length}</span>
      </h3>
      <table className="w-full text-sm">
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.id} className="hover:bg-transparent">
              {/* Baseline: the id and the first line beside it share it. */}
              <TableCell className="w-40 max-w-40 px-4 py-2 align-baseline break-all whitespace-normal">
                <code className="font-mono text-xs">{r.id}</code>
              </TableCell>
              <TableCell className="px-4 py-2 align-baseline whitespace-normal">{r.content}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </table>
    </section>
  )
}

function SchemaGroup({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <div>
      <h4 className="flex items-baseline gap-2 px-4 pt-3 pb-1 text-xs font-medium text-muted-foreground">
        {title}
        {note && <span className="font-normal">{note}</span>}
      </h4>
      <dl className="divide-y">{children}</dl>
    </div>
  )
}

/** A schema's fields in their declared order, as read-only rows. */
function FieldRows({ fields }: { fields: readonly FieldDef[] }) {
  return [...fields].sort(compareFields).map((f) => (
    <div key={f.key} className="grid grid-cols-[10rem_1fr] gap-3 px-4 py-2 text-sm">
      <dt className="min-w-0 truncate text-muted-foreground" title={f.description ?? undefined}>
        {f.label}
        {f.required && <span className="text-destructive">*</span>}
      </dt>
      <dd className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        <code className="font-mono text-xs">{f.key}</code>
        <span className="text-xs text-muted-foreground">{f.type}</span>
        {fieldNotes(f).map((n) => (
          <span key={n} className="text-xs text-muted-foreground">
            {n}
          </span>
        ))}
      </dd>
    </div>
  ))
}

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
        {m.blockedAt && (
          <Row label="Blocked">
            <DateOrDash date={m.blockedAt} />
            {m.blockedBy?.name && <span className="text-muted-foreground"> by {m.blockedBy.name}</span>}
            {m.blockReason && <div className="text-muted-foreground">{m.blockReason}</div>}
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

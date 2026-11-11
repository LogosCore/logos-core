import { Fragment, type ReactNode } from "react"
import { Badge } from "@/components/ui/badge"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { IdRows, InfoCard, NoteRow, SectionRow, type IdRow } from "@/components/ui/info-card"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { ModuleHealthIndicator } from "@/components/modules/module-health-indicator"
import { cn } from "@/lib/utils"
import { platformLabel } from "@/components/builds/build-values"
import { kindLabels } from "@/components/endpoints/endpoint-values"
import { compareFields, formatValue, type FieldDef } from "./field-values"
import { channelsText, fieldNotes, paramsNote, versionsText } from "./schema-notes"
import { commandKindsNote } from "@/components/minion-sessions/command-kinds"
import { ErrorCodes } from "@/components/builds/error-codes"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>
type Connection = NonNullable<ModuleDetailFieldsFragment["connection"]>
type Build = NonNullable<ModuleDetailFieldsFragment["build"]>

// The first column of the cards below, laid out together: a key of 17
// characters after its number on one line (checkin_interval,
// reconnect_backoff); a longer one wraps at an underscore.
const KEYS = "w-44"

/**
 * The minion connection settings this instance declared (read-only), per
 * connection kind it carries, each under a row saying which, and whether the
 * channel acts as the minion on it (agentless); a bind kind's target fields
 * after its own. Each field with its number, a minion config's key for it.
 * Operators fill them in for each connection of a build (a target's for each
 * target), or keep them as endpoints on an operation's Endpoints page.
 */
export function ConnectionSchema({ connection }: { connection: Connection }) {
  return (
    <InfoCard title="Minion connection settings" note={`schema v${connection.version}`} labelWidth={KEYS}>
      {connection.kinds.map((k) => {
        const label = kindLabels[k.kind] ?? k.kind
        return (
          <Fragment key={k.kind}>
            <SectionRow title={label} note={k.agentless ? `${k.kind} · acts as the minion` : k.kind} />
            {k.fields.length > 0 ? <IdRows rows={fieldRows(k.fields)} /> : <NoteRow>None.</NoteRow>}
            {k.target.length > 0 && (
              <>
                <SectionRow title={`${label}: each target`} note="filled in per target" />
                <IdRows rows={fieldRows(k.target)} />
              </>
            )}
          </Fragment>
        )
      })}
    </InfoCard>
  )
}

/**
 * The build parameters this factory instance declared (read-only), as a table
 * of each field's key and what it is: those every target takes, each
 * target's own, and the connection parameters per channel kind and connection
 * kind, each under a row saying which. The header says the schema version and the payload
 * crypto. Operators fill them in on an operation's Builds page.
 */
export function BuildParameters({ build }: { build: Build }) {
  const crypto = build.crypto ? `payload crypto ${build.crypto}` : "no payload crypto"
  return (
    <InfoCard title="Build parameters" note={`schema v${build.version} · ${crypto}`} labelWidth={KEYS}>
      {build.fields.length > 0 && (
        <>
          <SectionRow title="Every target" />
          <IdRows rows={fieldRows(build.fields)} />
        </>
      )}
      {build.targets
        .filter((t) => t.fields.length > 0)
        .map((t) => (
          <Fragment key={t.id}>
            <SectionRow title={`${t.label} only`} note={t.id} />
            <IdRows rows={fieldRows(t.fields)} />
          </Fragment>
        ))}
      {build.channels.flatMap((c) =>
        c.kinds.map((k) => (
          <Fragment key={`${c.moduleName}:${k.kind}`}>
            <SectionRow
              title={`${c.moduleName} ${k.kind} connection`}
              note={[c.num != null ? `channel ${c.num}` : "", `schema ${versionsText(c.versions)}`].filter(Boolean).join(" · ")}
            />
            {k.fields.length > 0 ? (
              <IdRows rows={fieldRows(k.fields)} />
            ) : (
              <NoteRow>None: the connection settings are used as they are.</NoteRow>
            )}
          </Fragment>
        )),
      )}
    </InfoCard>
  )
}

/** A schema's fields in their declared order, as rows: the number and the key, then the label, description, type and notes. */
function fieldRows(fields: readonly FieldDef[]): IdRow[] {
  return [...fields].sort(compareFields).map((f) => ({
    id: f.key,
    num: f.num,
    content: (
      <>
        <div>
          {f.label}
          {f.required && <span className="text-destructive">*</span>}
        </div>
        {f.description && <div className="text-xs text-muted-foreground">{f.description}</div>}
        <div className="text-xs text-muted-foreground">{[f.type, ...fieldNotes(f)].join(" · ")}</div>
      </>
    ),
  }))
}

/**
 * The targets this factory instance declared (read-only), what it can
 * produce: each one's id, and its label, description, format, platforms and
 * the channels it speaks.
 */
export function BuildTargets({ build }: { build: Build }) {
  return (
    <InfoCard title="Targets" note={build.targets.length} labelWidth={KEYS}>
      <IdRows
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
    </InfoCard>
  )
}

/**
 * The commands this factory instance declared (read-only), what its minions
 * can be built with: each one's op and id, and its label, description and
 * params by number (how a task names them), in the factory's order.
 * Operators pick a build's in the build form.
 */
export function BuildCommands({ build }: { build: Build }) {
  return (
    <InfoCard title="Commands" note={build.commands.length} labelWidth={KEYS}>
      <IdRows
        rows={build.commands.map((c) => ({
          id: c.id,
          num: c.op,
          content: (
            <>
              <div>{c.label}</div>
              {c.description && <div className="text-xs text-muted-foreground">{c.description}</div>}
              {paramsNote(c.params) && <div className="text-xs text-muted-foreground">{paramsNote(c.params)}</div>}
              {c.kinds.length > 0 && (
                <div className="text-xs text-muted-foreground">{commandKindsNote(c.kinds)?.text}</div>
              )}
            </>
          ),
        }))}
      />
    </InfoCard>
  )
}

/**
 * The error codes this factory instance declared (read-only): what its
 * minions' exit codes mean when they stop on an error.
 */
export function BuildErrorCodes({ build }: { build: Build }) {
  return <ErrorCodes codes={build.errorCodes} labelWidth={KEYS} />
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
  return date ? <FormattedDateTimeText date={date} /> : <>—</>
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

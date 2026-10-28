import type { FieldDef } from "@/components/modules/management/field-values"
import type { BuildQuery } from "@/graphql/gql/graphql"
import { isRecord, sameJSON } from "@/lib/json-edit"
import type { BuildTab } from "@/lib/record-modals"

// A finished or running build as its modal shows it (build-details.tsx):
// which tab it opens on, the record its JSON tab shows, and its values as
// Overview lists them.

export type BuildRecord = NonNullable<BuildQuery["build"]>

/**
 * The tab a build opens on when the URL names none: its pipeline while it
 * is queued or running, or when it failed or was cancelled (where it went
 * and why); its overview once it has succeeded.
 */
export function defaultBuildTab(state: string): BuildTab {
  return state === "SUCCEEDED" ? "overview" : "pipeline"
}

/**
 * The build as the JSON tab shows it: the record as the API gives it, the
 * build itself first and what it was given last. Its log is left out: up to
 * 64 KiB of text, which the Pipeline tab shows as a log. So are its
 * capabilities, until factories' command sets are worked out.
 */
export function buildRecordJson(b: BuildRecord): Record<string, unknown> {
  return {
    id: b.id,
    operationId: b.operationId,
    description: b.description,
    moduleName: b.moduleName,
    target: b.target,
    platform: b.platform,
    schemaVersion: b.schemaVersion,
    state: b.state,
    instance: b.instance ?? null,
    factoryVersion: b.factoryVersion ?? null,
    createdBy: b.createdBy,
    createdAt: b.createdAt,
    startedAt: b.startedAt ?? null,
    finishedAt: b.finishedAt ?? null,
    deadline: b.deadline,
    cancelledBy: b.cancelledBy ?? null,
    error: b.error ?? null,
    warnings: b.warnings,
    artifact: b.artifact ?? null,
    stages: b.stages,
    values: b.values,
    connections: b.connections,
    connectionDescriptions: b.connectionDescriptions,
  }
}

/** One value of a build (or of a connection's settings), as Overview lists it. */
export interface ValueRow {
  key: string
  /** Its field; null for a key the schema no longer has. */
  field: FieldDef | null
  /** undefined: the build was given none. */
  value: unknown
  /** Equal to its field's default (today's: the record keeps every default filled in). */
  isDefault: boolean
}

/**
 * values as rows, in their fields' order: each field with what the build
 * was given (none, for a field with no default it was not given), then keys
 * no field has any more.
 */
export function valueRows(fields: readonly FieldDef[], values: unknown): ValueRow[] {
  const record = isRecord(values) ? values : {}
  const rows: ValueRow[] = fields.map((f) => {
    const value = record[f.key]
    const hasDefault = f.default !== null && f.default !== undefined
    return { key: f.key, field: f, value, isDefault: value !== undefined && hasDefault && sameJSON(value, f.default) }
  })
  for (const [key, value] of Object.entries(record)) {
    if (!fields.some((f) => f.key === key)) rows.push({ key, field: null, value, isDefault: false })
  }
  return rows
}

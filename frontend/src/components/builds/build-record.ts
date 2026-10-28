import type { FieldDef } from "@/components/modules/management/field-values"
import type { BuildQuery } from "@/graphql/gql/graphql"
import { isRecord, sameJSON } from "@/lib/json-edit"
import type { BuildTab } from "@/lib/record-modals"
import { recordedConnections } from "@/components/builds/build-values"

// A finished or running build as its modal shows it (build-details.tsx):
// which tab it opens on, the record its JSON tab shows, and its values as
// Overview lists them. Which factory instance took the build is core's own
// business (it sends cancels there and takes reports only from there), so
// the modal leaves it out.

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
 * The build as the JSON tab shows it: what it takes to build it, as the New
 * build modal's JSON tab writes a build (build-json.ts), so a build's JSON
 * pastes into a new build's. Its factory, description, target, platform and
 * values, and its connections, each with its channel, name, description, the
 * channel's settings and the factory's values for it; every value as the
 * factory got it, defaults filled in. What came of the build (state, stages,
 * artifact, error, warnings, times, its log) is the other tabs'.
 */
export function buildRecordJson(b: BuildRecord): Record<string, unknown> {
  return {
    factory: { module_name: b.moduleName, schema_version: b.schemaVersion },
    description: b.description,
    target: b.target,
    platform: b.platform,
    values: b.values,
    connections: recordedConnections(b).map((c) => ({
      channel: c.moduleName,
      name: c.name,
      description: c.description,
      settings: c.params,
      values: c.values,
    })),
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
 * was given (none, for a field with no default that was not set), then keys
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

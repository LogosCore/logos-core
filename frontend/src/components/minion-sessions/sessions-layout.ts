import type {
  MinionSessionFieldsFragment,
  MinionSessionFilterInput,
  MinionSessionFilterOp,
  MinionSessionsLayoutFieldsFragment,
  SortDirection,
} from "@/graphql/gql/graphql"
import { platformLabel } from "@/components/builds/build-values"

// The Sessions table's model: the columns an operator can show (every
// property of a session), what each holds, and the layout they keep (columns
// in order, sort, filters), saved on their user for all operations. Core
// keeps the same column ids and kinds (models/minion_session_view.go) and
// does the filtering and sorting.

/** What a column holds, which decides its filter. */
export type SessionColumnKind = "text" | "list" | "enum" | "bool" | "number" | "time" | "id"

export type SessionColumnId =
  | "number"
  | "sessionId"
  | "addrs"
  | "host"
  | "user"
  | "domain"
  | "integrity"
  | "proc"
  | "pid"
  | "os"
  | "arch"
  | "version"
  | "lastSeen"
  | "opened"
  | "build"
  | "target"
  | "platform"
  | "buildDescription"
  | "buildId"
  | "staged"

export interface SessionColumnDef {
  id: SessionColumnId
  /** The header and menus. */
  label: string
  kind: SessionColumnKind
  sortable: boolean
  /** The first click on the header: newest or largest first for times. */
  initialDirection: SortDirection
  /** A CSS grid track. */
  width: string
  /**
   * The row's value as the filters and sort read it: a change to it can move
   * the row in or out of, or within, a filtered or sorted list.
   */
  value: (s: MinionSessionFieldsFragment) => unknown
}

const fr = (n: number) => `minmax(0,${n}fr)`

export const SESSION_COLUMNS: readonly SessionColumnDef[] = [
  { id: "number", label: "#", kind: "number", sortable: true, initialDirection: "DESC", width: "3.5rem", value: (s) => s.number },
  { id: "addrs", label: "Addresses", kind: "list", sortable: false, initialDirection: "ASC", width: fr(1), value: (s) => s.addrs },
  { id: "host", label: "Host", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.2), value: (s) => s.host },
  { id: "user", label: "User", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1), value: (s) => s.user },
  { id: "domain", label: "Domain", kind: "text", sortable: true, initialDirection: "ASC", width: fr(0.9), value: (s) => s.domain },
  { id: "integrity", label: "Integrity", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.7), value: (s) => s.integrity },
  { id: "proc", label: "Process", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.1), value: (s) => s.proc },
  { id: "pid", label: "PID", kind: "number", sortable: true, initialDirection: "ASC", width: fr(0.5), value: (s) => s.pid },
  { id: "os", label: "OS", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.9), value: (s) => s.os },
  { id: "arch", label: "Arch", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.6), value: (s) => s.arch },
  { id: "version", label: "Minion version", kind: "enum", sortable: true, initialDirection: "DESC", width: fr(0.7), value: (s) => s.version },
  { id: "lastSeen", label: "Last seen", kind: "time", sortable: true, initialDirection: "DESC", width: fr(0.9), value: (s) => s.lastSeenAt },
  { id: "opened", label: "Opened", kind: "time", sortable: true, initialDirection: "DESC", width: fr(0.9), value: (s) => s.createdAt },
  { id: "build", label: "Build", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(1.4), value: (s) => s.moduleName },
  { id: "target", label: "Target", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.8), value: (s) => s.target },
  { id: "platform", label: "Build platform", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.9), value: (s) => s.platform },
  { id: "buildDescription", label: "Build description", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.4), value: (s) => s.buildDescription },
  { id: "buildId", label: "Build id", kind: "id", sortable: false, initialDirection: "ASC", width: fr(1.6), value: (s) => s.buildId },
  { id: "sessionId", label: "Session id", kind: "id", sortable: false, initialDirection: "ASC", width: fr(1.6), value: (s) => s.id },
  { id: "staged", label: "Staged", kind: "bool", sortable: true, initialDirection: "DESC", width: fr(0.5), value: (s) => s.staged },
]

export const SESSION_COLUMN_BY_ID: ReadonlyMap<string, SessionColumnDef> = new Map(
  SESSION_COLUMNS.map((c) => [c.id, c]),
)

export interface SessionSort {
  column: SessionColumnId
  direction: SortDirection
}

/** One column filter: the GraphQL input. One per column in the table. */
export type SessionFilter = MinionSessionFilterInput & { column: SessionColumnId }

export interface SessionsLayout {
  columns: SessionColumnId[]
  /** null: newest first (core's default). */
  sort: SessionSort | null
  filters: SessionFilter[]
}

export const DEFAULT_SESSIONS_LAYOUT: SessionsLayout = {
  columns: ["number", "addrs", "host", "user", "proc", "os", "lastSeen", "build"],
  sort: null,
  filters: [],
}

const isColumn = (id: string): id is SessionColumnId => SESSION_COLUMN_BY_ID.has(id)

/**
 * A stored layout as the table uses it: the default when there is none, and
 * anything this build does not know (a column renamed or dropped since)
 * left out, so an old layout never breaks the page.
 */
export function readSessionsLayout(stored: MinionSessionsLayoutFieldsFragment | null | undefined): SessionsLayout {
  if (!stored) return DEFAULT_SESSIONS_LAYOUT
  const columns = [...new Set(stored.columns.filter(isColumn))]
  const sort =
    stored.sort && isColumn(stored.sort.column) && SESSION_COLUMN_BY_ID.get(stored.sort.column)!.sortable
      ? { column: stored.sort.column, direction: stored.sort.direction }
      : null
  const filters: SessionFilter[] = []
  for (const f of stored.filters) {
    if (!isColumn(f.column) || filters.some((g) => g.column === f.column)) continue
    filters.push(withoutNulls({ ...f, column: f.column }))
  }
  return { columns: columns.length > 0 ? columns : DEFAULT_SESSIONS_LAYOUT.columns, sort, filters }
}

/** The layout as the mutation takes it. */
export function sessionsLayoutInput(layout: SessionsLayout) {
  return { columns: layout.columns, sort: layout.sort, filters: layout.filters }
}

function withoutNulls(f: SessionFilter): SessionFilter {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(f)) {
    if (v !== null && v !== undefined && k !== "__typename") out[k] = v
  }
  return out as SessionFilter
}

// --- layout edits ---

export function moveColumn(layout: SessionsLayout, from: SessionColumnId, to: SessionColumnId): SessionsLayout {
  const columns = [...layout.columns]
  const i = columns.indexOf(from)
  const j = columns.indexOf(to)
  if (i < 0 || j < 0 || i === j) return layout
  columns.splice(i, 1)
  columns.splice(j, 0, from)
  return { ...layout, columns }
}

/** Moves a column one place left (-1) or right (1). */
export function shiftColumn(layout: SessionsLayout, id: SessionColumnId, by: -1 | 1): SessionsLayout {
  const i = layout.columns.indexOf(id)
  const j = i + by
  if (i < 0 || j < 0 || j >= layout.columns.length) return layout
  return moveColumn(layout, id, layout.columns[j])
}

/**
 * Shows a hidden column (at the end) or hides a shown one. The last column
 * stays. Hiding a column keeps its filter and sort: they still apply, and the
 * chips above the table say so.
 */
export function toggleColumn(layout: SessionsLayout, id: SessionColumnId): SessionsLayout {
  if (layout.columns.includes(id)) {
    if (layout.columns.length === 1) return layout
    return { ...layout, columns: layout.columns.filter((c) => c !== id) }
  }
  return { ...layout, columns: [...layout.columns, id] }
}

export function setSort(layout: SessionsLayout, sort: SessionSort | null): SessionsLayout {
  return { ...layout, sort }
}

/** A header click: the column's first direction, then the other, then back to newest first. */
export function cycleSort(layout: SessionsLayout, id: SessionColumnId): SessionsLayout {
  const col = SESSION_COLUMN_BY_ID.get(id)
  if (!col?.sortable) return layout
  const cur = layout.sort
  if (cur?.column !== id) return setSort(layout, { column: id, direction: col.initialDirection })
  if (cur.direction === col.initialDirection) {
    return setSort(layout, { column: id, direction: cur.direction === "ASC" ? "DESC" : "ASC" })
  }
  return setSort(layout, null)
}

/** Sets a column's filter, or removes it with null. */
export function setFilter(layout: SessionsLayout, column: SessionColumnId, filter: SessionFilter | null): SessionsLayout {
  const rest = layout.filters.filter((f) => f.column !== column)
  return { ...layout, filters: filter ? [...rest, withoutNulls(filter)] : rest }
}

export function filterOf(layout: SessionsLayout, column: SessionColumnId): SessionFilter | undefined {
  return layout.filters.find((f) => f.column === column)
}

// --- what a filter says ---

export const FILTER_OPS: Record<SessionColumnKind, MinionSessionFilterOp[]> = {
  text: ["CONTAINS", "NOT_CONTAINS", "EMPTY", "NOT_EMPTY"],
  list: ["CONTAINS", "NOT_CONTAINS", "EMPTY", "NOT_EMPTY"],
  enum: ["IN", "NOT_IN"],
  bool: ["IN", "NOT_IN"],
  number: ["RANGE"],
  time: ["WITHIN", "OLDER_THAN", "NEVER", "RANGE"],
  id: ["EQUALS"],
}

/** Seconds as the filters say them: "90s", "5m", "1h", "2d". */
export function formatSeconds(seconds: number): string {
  for (const [unit, n] of [
    ["d", 86400],
    ["h", 3600],
    ["m", 60],
  ] as const) {
    if (seconds >= n && seconds % n === 0) return `${seconds / n}${unit}`
  }
  return `${seconds}s`
}

/** A filter's value in words: what its chip says after the column. */
export function describeFilter(f: SessionFilter): string {
  const col = SESSION_COLUMN_BY_ID.get(f.column)
  const value = (v: string) => enumLabel(f.column, v)
  switch (f.op) {
    case "CONTAINS":
      return `contains "${f.text ?? ""}"`
    case "NOT_CONTAINS":
      return `does not contain "${f.text ?? ""}"`
    case "EMPTY":
      return "is empty"
    case "NOT_EMPTY":
      return "is not empty"
    case "IN":
      return `is ${(f.values ?? []).map(value).join(", ")}`
    case "NOT_IN":
      return `is not ${(f.values ?? []).map(value).join(", ")}`
    case "EQUALS":
      return `is ${f.text ?? ""}`
    case "WITHIN":
      return `within ${formatSeconds(f.seconds ?? 0)}`
    case "OLDER_THAN":
      return `over ${formatSeconds(f.seconds ?? 0)} ago`
    case "NEVER":
      return col?.id === "lastSeen" ? "never (no check-in yet)" : "never"
    case "RANGE":
      if (col?.kind === "time") {
        const at = (t: string) => new Date(t).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
        if (f.from && f.to) return `between ${at(f.from)} and ${at(f.to)}`
        return f.from ? `since ${at(f.from)}` : `until ${at(f.to!)}`
      }
      if (f.min != null && f.max != null) return f.min === f.max ? `= ${f.min}` : `${f.min}–${f.max}`
      return f.min != null ? `≥ ${f.min}` : `≤ ${f.max}`
  }
  return f.op
}

/** How an enum or bool value reads: platforms named, staged as yes / no. */
export function enumLabel(column: SessionColumnId, value: string): string {
  if (column === "staged") return value === "true" ? "staged" : "not staged"
  if (column === "os") return platformLabel(`${value}/any`).replace(" (any arch)", "")
  if (column === "platform") return platformLabel(value)
  return value
}

/** Whether a filter changes with the clock (so the list goes stale without events). */
export function isClockFilter(f: SessionFilter): boolean {
  return f.op === "WITHIN" || f.op === "OLDER_THAN"
}

/**
 * The columns a list's order and membership depend on: its filters' and its
 * sort's. An update that leaves their values alone patches the row in place.
 */
export function dependentColumns(params: { filters: SessionFilter[]; sort: SessionSort | null }): SessionColumnId[] {
  const out = new Set<SessionColumnId>(params.filters.map((f) => f.column))
  out.add(params.sort?.column ?? "opened")
  return [...out]
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/** Whether a filter has what its operation needs (core checks it again). */
export function isComplete(f: SessionFilter, column: SessionColumnDef): boolean {
  switch (f.op) {
    case "CONTAINS":
    case "NOT_CONTAINS":
      return !!f.text
    case "EQUALS":
      return UUID.test(f.text ?? "")
    case "IN":
    case "NOT_IN":
      return (f.values?.length ?? 0) > 0
    case "RANGE":
      return column.kind === "time" ? !!(f.from || f.to) : f.min != null || f.max != null
    case "WITHIN":
    case "OLDER_THAN":
      return (f.seconds ?? 0) > 0
  }
  return true
}

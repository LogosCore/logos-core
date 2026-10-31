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
  | "peer"
  | "host"
  | "user"
  | "domain"
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
  | "description"
  | "buildId"
  | "staged"
  | "persisted"
  | "restarts"

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
  // Sorts by the address each row shows (MinionSession.address).
  { id: "addrs", label: "Addresses", kind: "list", sortable: true, initialDirection: "ASC", width: fr(1), value: (s) => [s.address, s.addrs] },
  // The address the latest message came from (the channel saw it); sorts by
  // that one, filters on any it was seen from. A check-in from the same
  // address moves nothing.
  {
    id: "peer",
    label: "External address",
    kind: "list",
    sortable: true,
    initialDirection: "ASC",
    width: fr(1),
    value: (s) => [s.peerAddress, s.peers.map((p) => p.address)],
  },
  { id: "host", label: "Host", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.2), value: (s) => s.host },
  // The user, with the integrity level it runs at: its filter matches either.
  { id: "user", label: "User", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1), value: (s) => [s.user, s.integrity] },
  { id: "domain", label: "Domain", kind: "text", sortable: true, initialDirection: "ASC", width: fr(0.9), value: (s) => s.domain },
  { id: "proc", label: "Process", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.1), value: (s) => s.proc },
  { id: "pid", label: "PID", kind: "number", sortable: true, initialDirection: "ASC", width: fr(0.5), value: (s) => s.pid },
  { id: "os", label: "OS", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.9), value: (s) => s.os },
  { id: "arch", label: "Arch", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.6), value: (s) => s.arch },
  { id: "version", label: "Minion version", kind: "enum", sortable: true, initialDirection: "DESC", width: fr(0.7), value: (s) => s.version },
  { id: "lastSeen", label: "Last seen", kind: "time", sortable: true, initialDirection: "DESC", width: fr(0.9), value: (s) => [s.lastSeenAt, s.lateAt, s.lostAt] },
  { id: "opened", label: "Opened", kind: "time", sortable: true, initialDirection: "DESC", width: fr(0.9), value: (s) => s.createdAt },
  { id: "build", label: "Build", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(1.4), value: (s) => s.moduleName },
  { id: "target", label: "Target", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.8), value: (s) => s.target },
  { id: "platform", label: "Build platform", kind: "enum", sortable: true, initialDirection: "ASC", width: fr(0.9), value: (s) => s.platform },
  { id: "description", label: "Description", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.4), value: (s) => s.description },
  { id: "buildDescription", label: "Build description", kind: "text", sortable: true, initialDirection: "ASC", width: fr(1.4), value: (s) => s.buildDescription },
  { id: "buildId", label: "Build id", kind: "id", sortable: false, initialDirection: "ASC", width: fr(1.6), value: (s) => s.buildId },
  { id: "sessionId", label: "Session id", kind: "id", sortable: false, initialDirection: "ASC", width: fr(1.6), value: (s) => s.id },
  { id: "persisted", label: "Persisted", kind: "bool", sortable: true, initialDirection: "DESC", width: fr(0.6), value: (s) => s.persisted },
  { id: "restarts", label: "Restarts", kind: "number", sortable: true, initialDirection: "DESC", width: fr(0.6), value: (s) => s.restartCount },
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
  /** The share of the page's height the list takes above the session; null: DEFAULT_SPLIT. */
  split: number | null
  /** The share of a session console's width its task list takes; null: DEFAULT_CONSOLE_SPLIT. */
  consoleSplit: number | null
}

export const DEFAULT_SESSIONS_LAYOUT: SessionsLayout = {
  columns: ["number", "peer", "addrs", "user", "host", "domain", "arch", "pid", "proc", "os", "lastSeen", "build", "description"],
  sort: null,
  filters: [],
  split: null,
  consoleSplit: null,
}

/** Where the divider sits by default, and the range it keeps to (core's too). */
export const DEFAULT_SPLIT = 0.6
export const MIN_SPLIT = 0.1
export const MAX_SPLIT = 0.9
export const DEFAULT_CONSOLE_SPLIT = 0.3

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
  const share = (v: number | null | undefined) => (v != null ? Math.min(MAX_SPLIT, Math.max(MIN_SPLIT, v)) : null)
  return {
    columns: columns.length > 0 ? columns : DEFAULT_SESSIONS_LAYOUT.columns,
    sort,
    filters,
    split: share(stored.split),
    consoleSplit: share(stored.consoleSplit),
  }
}

/** The layout as the mutation takes it. */
export function sessionsLayoutInput(layout: SessionsLayout) {
  return { columns: layout.columns, sort: layout.sort, filters: layout.filters, split: layout.split, consoleSplit: layout.consoleSplit }
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
 * Shows a hidden column, right after the column `after` (a header's menu
 * adds next to its column) or at the end, or hides a shown one. The last
 * column stays. Hiding a column keeps its filter and sort: they still apply,
 * and the chips above the table say so.
 */
export function toggleColumn(layout: SessionsLayout, id: SessionColumnId, after?: SessionColumnId | null): SessionsLayout {
  if (layout.columns.includes(id)) {
    if (layout.columns.length === 1) return layout
    return { ...layout, columns: layout.columns.filter((c) => c !== id) }
  }
  const at = after ? layout.columns.indexOf(after) : -1
  if (at < 0) return { ...layout, columns: [...layout.columns, id] }
  return { ...layout, columns: [...layout.columns.slice(0, at + 1), id, ...layout.columns.slice(at + 1)] }
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
  time: ["WITHIN", "OLDER_THAN", "NEVER", "RANGE", "LIVE", "LATE", "LOST"],
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
    case "LIVE":
      return "on time"
    case "LATE":
      return "late"
    case "LOST":
      return "lost"
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
  if (column === "persisted") return value === "true" ? "persisted" : "not persisted"
  if (column === "os") return platformLabel(`${value}/any`).replace(" (any arch)", "")
  if (column === "platform") return platformLabel(value)
  return value
}

/** Whether a filter changes with the clock (so the list goes stale without events). */
export function isClockFilter(f: SessionFilter): boolean {
  return f.op === "WITHIN" || f.op === "OLDER_THAN" || f.op === "LIVE" || f.op === "LATE" || f.op === "LOST"
}

/**
 * Where a session stands by its schedule: on time, late (a check-in
 * overdue), lost (three missed), or unknown (it never said one).
 */
export function lateness(
  s: Pick<MinionSessionFieldsFragment, "lateAt" | "lostAt">,
  now: number,
): "live" | "late" | "lost" | null {
  if (!s.lateAt) return null
  if (now < Date.parse(s.lateAt)) return "live"
  return s.lostAt && now >= Date.parse(s.lostAt) ? "lost" : "late"
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

/**
 * What the address filters and the search look for, as a finder over a
 * session's addresses: the first one that contains any of it, case aside.
 * A row a filter brought in shows the address that matched.
 */
export function addressMatcher(filters: SessionFilter[], search: string | null): (addrs: readonly string[]) => string | undefined {
  const needles = filters
    .filter((f) => f.column === "addrs" && f.op === "CONTAINS" && f.text)
    .map((f) => f.text!.toLowerCase())
  const term = search?.trim().replace(/^"(.*)"$/, "$1").toLowerCase()
  if (term) needles.push(term)
  if (needles.length === 0) return () => undefined
  return (addrs) => addrs.find((a) => needles.some((n) => a.toLowerCase().includes(n)))
}

/**
 * The address a row shows: one someone picked, else one the filters or the
 * search matched, else core's guess.
 */
export function shownAddress(
  s: Pick<MinionSessionFieldsFragment, "address" | "addressPicked" | "addrs">,
  match: (addrs: readonly string[]) => string | undefined,
): string | null {
  if (s.addressPicked && s.address) return s.address
  return match(s.addrs) ?? s.address ?? s.addrs[0] ?? null
}

/**
 * How long ago, for the last day, at one unit: "just now" for up to 3
 * seconds (a minion checking in every second stays there), then seconds
 * under a minute ("47s ago"), minutes under an hour ("4m ago"), hours
 * ("3h ago"); null from a day on (the date and time say it better). The
 * hover gives it exactly (formatAgoExact).
 */
export function formatAgo(diffMs: number): string | null {
  const s = Math.max(0, Math.floor(diffMs / 1000))
  if (s >= 86400) return null
  if (s <= 3) return "just now"
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  return `${Math.floor(s / 3600)}h ago`
}

/** How long ago to the second, for a hover: "4m 05s ago", "3h 12m 09s ago", "2d 3h ago". */
export function formatAgoExact(diffMs: number): string {
  const s = Math.max(0, Math.floor(diffMs / 1000))
  const pad = (n: number) => String(n).padStart(2, "0")
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ${pad(s % 60)}s ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ${pad(Math.floor(s / 60) % 60)}m ${pad(s % 60)}s ago`
  return `${Math.floor(s / 86400)}d ${Math.floor(s / 3600) % 24}h ago`
}

/** Milliseconds as the schedule says them: "800ms", "60s", "5m", "1h 30m". */
export function formatInterval(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = Math.round(ms / 1000)
  if (s < 120) return `${s}s`
  const m = Math.round(s / 60)
  if (m < 120) return `${m}m`
  const h = Math.floor(m / 60)
  return m % 60 ? `${h}h ${m % 60}m` : `${h}h`
}

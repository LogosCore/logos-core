// Turns the two things known about an agent into the single row the agents
// grid renders, and does the filtering and sorting over it.
//
// An operator thinks in agents, not in keys: "what is this thing allowed to do"
// and "what has it been doing" are one question asked of one object. The server
// answers them from two places — myAgentKeys carries identity and the ceiling,
// myAgentActivitySummary carries what was actually called — so the join happens
// here rather than in each card.
//
// Pure, so the grid stays a rendering concern and this is what gets tested.

import type { DataTableSort } from "@/lib/data-table-sort"
import type { OperationRole } from "@/graphql/gql/graphql"

export type AgentSortField = "LAST_SEEN" | "NAME" | "CREATED"

export type AgentStatusFilter =
  | null
  | "active"
  | "paused"
  | "writable"
  | "readonly"
  | "unused"

export interface AgentKeyInput {
  id: string
  keyId: string
  name: string
  enabled: boolean
  maxRole: OperationRole
  allowWrites: boolean
  operationScopes: { id: string; name: string }[]
  lastUsedAt?: string | null
  createdAt: string
}

export interface AgentActivityInput {
  agentKeyId: string
  actions: number
  operations: number
  lastSeen: string
}

export interface AgentRow {
  id: string
  keyId: string
  name: string
  enabled: boolean
  maxRole: OperationRole
  allowWrites: boolean
  /** Empty means "every operation the owner belongs to" — the widest setting. */
  operationScopes: { id: string; name: string }[]
  createdAt: string
  /**
   * Whether this key can actually change anything. A viewer-capped key with
   * writes switched on cannot: the operation role refuses them regardless, and
   * the create form says so. Deriving it once here keeps the badge from
   * claiming a capability the server will not honour.
   */
  canWrite: boolean
  /** Calls recorded for this key, across every operation. */
  actions: number
  /** Operations it has actually touched — not the ones it is scoped to. */
  operationsTouched: number
  /** Null only when the key has never been used at all. */
  lastSeen: string | null
  neverUsed: boolean
}

export function buildAgentRows(
  keys: AgentKeyInput[],
  activity: AgentActivityInput[],
): AgentRow[] {
  const byKey = new Map(activity.map((a) => [a.agentKeyId, a]))

  return keys.map((key) => {
    const seen = byKey.get(key.id)
    // Two independent facts: lastUsedAt is stamped when a token authenticates,
    // lastSeen comes from recorded calls. A key can authenticate and call
    // nothing, so the later of the two is the honest answer to "when was this
    // last active" — picking either one alone can report a key as quieter than
    // it is.
    const lastSeen = newer(seen?.lastSeen ?? null, key.lastUsedAt ?? null)

    return {
      id: key.id,
      keyId: key.keyId,
      name: key.name,
      enabled: key.enabled,
      maxRole: key.maxRole,
      allowWrites: key.allowWrites,
      operationScopes: key.operationScopes,
      createdAt: key.createdAt,
      canWrite: key.allowWrites && key.maxRole !== "VIEWER",
      actions: seen?.actions ?? 0,
      operationsTouched: seen?.operations ?? 0,
      lastSeen,
      neverUsed: lastSeen == null,
    }
  })
}

export function filterAgentRows(
  rows: AgentRow[],
  search: string,
  status: AgentStatusFilter,
): AgentRow[] {
  const query = search.trim().toLowerCase()
  return rows.filter((row) => {
    if (!matchesStatus(row, status)) return false
    if (!query) return true
    return (
      row.name.toLowerCase().includes(query) ||
      row.keyId.toLowerCase().includes(query) ||
      row.operationScopes.some((op) => op.name.toLowerCase().includes(query))
    )
  })
}

function matchesStatus(row: AgentRow, status: AgentStatusFilter): boolean {
  switch (status) {
    case "active":
      return row.enabled
    case "paused":
      return !row.enabled
    case "writable":
      return row.canWrite
    case "readonly":
      return !row.canWrite
    case "unused":
      return row.neverUsed
    default:
      return true
  }
}

export function sortAgentRows(
  rows: AgentRow[],
  sort: DataTableSort<AgentSortField>,
): AgentRow[] {
  const factor = sort.direction === "ASC" ? 1 : -1

  return [...rows].sort((a, b) => {
    if (sort.field === "LAST_SEEN") {
      // A never-used key is not "the oldest one" — it has no date at all.
      // Sorting it to the bottom in both directions keeps the keys that have
      // activity where an operator sorting by activity is looking, rather than
      // burying them under a pile of keys nothing ever authenticated with.
      if (a.lastSeen == null || b.lastSeen == null) {
        if (a.lastSeen === b.lastSeen) return a.name.localeCompare(b.name)
        return a.lastSeen == null ? 1 : -1
      }
      return (
        (Date.parse(a.lastSeen) - Date.parse(b.lastSeen) ||
          a.name.localeCompare(b.name)) * factor
      )
    }

    if (sort.field === "CREATED") {
      return (
        (Date.parse(a.createdAt) - Date.parse(b.createdAt) ||
          a.name.localeCompare(b.name)) * factor
      )
    }

    return a.name.localeCompare(b.name) * factor
  })
}

/** The later of two optional timestamps; null when both are absent. */
function newer(a: string | null, b: string | null): string | null {
  if (a == null) return b
  if (b == null) return a
  return Date.parse(a) >= Date.parse(b) ? a : b
}

export interface AgentCounts {
  total: number
  active: number
  paused: number
  unused: number
}

/**
 * How many agents are in a state worth acting on, for the toolbar's counts.
 * Kept beside the filters so "paused" means the same thing in the summary line
 * and in the dropdown.
 */
export function countAgents(rows: AgentRow[]): AgentCounts {
  return {
    total: rows.length,
    active: rows.filter((r) => r.enabled).length,
    paused: rows.filter((r) => !r.enabled).length,
    unused: rows.filter((r) => r.neverUsed).length,
  }
}

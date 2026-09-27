import { useCallback, useMemo } from "react"
import { useSearchParams } from "react-router"
import type { AgentActionOutcome } from "@/graphql/gql/graphql"

// Filters for the agent activity tab, held in the URL.
//
// The URL rather than a store because these filters are the thing worth
// linking: a card on the Agents tab points at "what this agent did", and the
// answer has to survive being sent to a teammate or reopened from history. The
// previous store-backed version could not be addressed at all.
//
// Nothing is restored from storage, and writes use `replace` so the back button
// still leaves the page instead of walking a trail of filter changes. A filter
// quietly restored from a previous visit would hide activity from an operator
// who had forgotten they set it, which is a poor property for an audit view.

// The two tab routes. Here rather than in the page so the link builder below and
// the page's own tab triggers cannot disagree about where a tab lives.
export const AGENTS_PATH = "/agents"
export const AGENTS_ACTIVITY_PATH = "/agents/activity"

const AGENT_PARAM = "agent"
const OPERATION_PARAM = "op"
const WRITES_PARAM = "writes"
const REFUSED_PARAM = "refused"

const FILTER_PARAMS = [
  AGENT_PARAM,
  OPERATION_PARAM,
  WRITES_PARAM,
  REFUSED_PARAM,
] as const

// Agent keys and operations are both UUID-identified. A cheap defensive check
// so a hand-edited URL narrows to nothing recognisable rather than sending
// garbage to the server as a filter.
const UUID_RE =
  /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/

export interface AgentActivityFilters {
  agentKeyId: string | null
  // Narrows an otherwise cross-operation feed. Not the frame — the trail
  // belongs to the operator, not to any one engagement.
  operationId: string | null
  writesOnly: boolean
  refusedOnly: boolean
}

function parseId(raw: string | null): string | null {
  if (raw == null) return null
  const trimmed = raw.trim()
  return UUID_RE.test(trimmed) ? trimmed : null
}

export function useAgentActivityParams() {
  const [searchParams, setSearchParams] = useSearchParams()

  const filters = useMemo<AgentActivityFilters>(
    () => ({
      agentKeyId: parseId(searchParams.get(AGENT_PARAM)),
      operationId: parseId(searchParams.get(OPERATION_PARAM)),
      writesOnly: searchParams.get(WRITES_PARAM) === "1",
      refusedOnly: searchParams.get(REFUSED_PARAM) === "1",
    }),
    [searchParams],
  )

  const write = useCallback(
    (mutate: (params: URLSearchParams) => void) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev)
          mutate(params)
          return params
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const setParam = useCallback(
    (key: string, value: string | null) => {
      write((params) => {
        if (value == null) params.delete(key)
        else params.set(key, value)
      })
    },
    [write],
  )

  const setAgentKeyId = useCallback(
    (id: string | null) => setParam(AGENT_PARAM, id),
    [setParam],
  )

  const setOperationId = useCallback(
    (id: string | null) => setParam(OPERATION_PARAM, id),
    [setParam],
  )

  const toggleWritesOnly = useCallback(
    () => setParam(WRITES_PARAM, filters.writesOnly ? null : "1"),
    [setParam, filters.writesOnly],
  )

  const toggleRefusedOnly = useCallback(
    () => setParam(REFUSED_PARAM, filters.refusedOnly ? null : "1"),
    [setParam, filters.refusedOnly],
  )

  // Only these four keys: the page may carry unrelated params one day, and
  // "clear filters" should not take them with it.
  const clearFilters = useCallback(() => {
    write((params) => {
      for (const key of FILTER_PARAMS) params.delete(key)
    })
  }, [write])

  const filtered =
    filters.agentKeyId != null ||
    filters.operationId != null ||
    filters.writesOnly ||
    filters.refusedOnly

  return {
    filters,
    filtered,
    setAgentKeyId,
    setOperationId,
    toggleWritesOnly,
    toggleRefusedOnly,
    clearFilters,
  }
}

/**
 * Link target for "what has this agent been doing" — the Agents tab's way into
 * the activity tab. Kept here so the param names have exactly one definition.
 */
export function agentActivityPath(agentKeyId: string): string {
  return `${AGENTS_ACTIVITY_PATH}?${AGENT_PARAM}=${encodeURIComponent(agentKeyId)}`
}

// outcomesFor turns the refusals toggle into the query's enum list. Kept beside
// the filters so the tab and the query agree on what "refused only" means.
export function outcomesFor(refusedOnly: boolean): AgentActionOutcome[] | null {
  return refusedOnly ? ["REFUSED"] : null
}

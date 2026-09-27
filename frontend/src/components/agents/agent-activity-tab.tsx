import { useMemo } from "react"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import {
  useInfiniteAgentActions,
  useMyAgentActivitySummary,
} from "@/graphql/hooks/agent-actions"
import {
  outcomesFor,
  useAgentActivityParams,
} from "@/hooks/use-agent-activity-params"
import { AgentActivityToolbar } from "@/components/agent-activity/agent-activity-toolbar"
import { AgentActionsTable } from "@/components/agent-activity/agent-actions-table"

/**
 * What the operator's own agents have been doing — across every operation they
 * touched.
 *
 * Reads are included, which is the point: the timeline shows what an agent
 * changed, this shows what it looked at. Operation is a filter here, not the
 * frame — you delegate to a key, not to an engagement, and one key can reach
 * several.
 */
export function AgentActivityTab() {
  const { filters } = useAgentActivityParams()

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } =
    useInfiniteAgentActions({
      agentKeyId: filters.agentKeyId,
      operationId: filters.operationId,
      writesOnly: filters.writesOnly || null,
      outcomes: outcomesFor(filters.refusedOnly),
    })

  const actions = useConnectionNodes(data, (p) => p.myAgentActions)
  const totalCount = data?.pages[0]?.myAgentActions.totalCount ?? 0

  const summary = useMyAgentActivitySummary()
  const agents = summary.data?.myAgentActivitySummary ?? []

  // Operations are derived from what has loaded rather than fetched: the only
  // ones worth filtering by are the ones an agent has actually touched.
  const operations = useMemo(() => {
    const seen = new Map<string, string>()
    for (const action of actions) {
      if (action.operation) seen.set(action.operation.id, action.operation.name)
    }
    return [...seen].map(([id, name]) => ({ id, name }))
  }, [actions])

  return (
    <div className="flex flex-1 flex-col gap-2">
      <AgentActivityToolbar
        agents={agents}
        operations={operations}
        totalCount={totalCount}
      />
      <AgentActionsTable
        actions={actions}
        isLoading={isLoading}
        isFetchingNextPage={isFetchingNextPage}
        hasNextPage={!!hasNextPage}
        fetchNextPage={fetchNextPage}
      />
    </div>
  )
}

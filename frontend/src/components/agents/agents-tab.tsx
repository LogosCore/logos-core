import { useMemo } from "react"
import { AgentsToolbar } from "@/components/agents/agents-toolbar"
import { AgentsGrid } from "@/components/agents/agents-grid"
import { AgentsEmptyState } from "@/components/agents/agents-empty-state"
import { AgentFormSheet } from "@/components/agents/agent-form-sheet"
import { useMyAgentKeys } from "@/graphql/hooks/agent-keys"
import { useMyAgentActivitySummary } from "@/graphql/hooks/agent-actions"
import { useAgentStore } from "@/stores/agents"
import {
  buildAgentRows,
  countAgents,
  filterAgentRows,
  sortAgentRows,
} from "@/lib/agent-rows"

/**
 * The agents an operator has delegated to, and what each one may reach.
 *
 * Identity comes from myAgentKeys and activity from myAgentActivitySummary,
 * joined client-side — see lib/agent-rows. Both are already fetched elsewhere in
 * the app, so the tab adds no request beyond what the keys list always cost.
 */
export function AgentsTab() {
  const search = useAgentStore((s) => s.search)
  const statusFilter = useAgentStore((s) => s.statusFilter)
  const sort = useAgentStore((s) => s.sort)
  const setSearch = useAgentStore((s) => s.setSearch)
  const setStatusFilter = useAgentStore((s) => s.setStatusFilter)

  const { data: keys, isLoading } = useMyAgentKeys()
  // Not gated on the summary: the keys are what the page is about, and a card
  // that appears now and fills in its call count a moment later beats an empty
  // page waiting on a count.
  const { data: summary } = useMyAgentActivitySummary()

  const allRows = useMemo(
    () =>
      buildAgentRows(
        keys?.myAgentKeys ?? [],
        summary?.myAgentActivitySummary ?? [],
      ),
    [keys, summary],
  )

  const rows = useMemo(
    () => sortAgentRows(filterAgentRows(allRows, search, statusFilter), sort),
    [allRows, search, statusFilter, sort],
  )

  const counts = useMemo(() => countAgents(allRows), [allRows])
  const filtered = search.trim() !== "" || statusFilter != null

  function clearFilters() {
    setSearch("")
    setStatusFilter(null)
  }

  // Nothing at all is an onboarding problem, not a list problem, so the toolbar
  // and its filters stay out of the way until there is something to filter.
  const showEmptyState = !isLoading && allRows.length === 0

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      {showEmptyState ? (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <AgentsEmptyState />
        </div>
      ) : (
        <>
          <AgentsToolbar counts={counts} />
          {/* The grid scrolls, not the page: the shell is locked to one
              viewport (see app-layout), so a page that grows with the number of
              agents would run off the bottom with no way to reach it. The
              toolbar stays put above it, which is also where it is wanted. */}
          <div className="min-h-0 flex-1 overflow-y-auto pb-1">
            <AgentsGrid
              rows={rows}
              isLoading={isLoading}
              filtered={filtered}
              onClearFilters={clearFilters}
            />
          </div>
        </>
      )}

      {/* Outside the branch: the empty state's "New agent" opens it too. */}
      <AgentFormSheet rows={allRows} />
    </div>
  )
}

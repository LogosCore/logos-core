import { SearchXIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { AgentCard } from "@/components/agents/agent-card"
import type { AgentRow } from "@/lib/agent-rows"

interface AgentsGridProps {
  rows: AgentRow[]
  isLoading: boolean
  /** True when a search or status filter is narrowing the list. */
  filtered: boolean
  onClearFilters: () => void
}

export function AgentsGrid({
  rows,
  isLoading,
  filtered,
  onClearFilters,
}: AgentsGridProps) {
  if (isLoading) {
    return (
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-44 w-full" />
        ))}
      </div>
    )
  }

  // The caller renders the onboarding empty state when there are no agents at
  // all, so nothing here means the filters excluded everything.
  if (rows.length === 0 && filtered) {
    return (
      <div className="flex flex-col items-center gap-2 rounded-md border border-dashed p-10 text-center">
        <SearchXIcon className="size-6 text-muted-foreground/60" />
        <p className="text-sm text-muted-foreground">No agents match.</p>
        <Button size="sm" variant="outline" onClick={onClearFilters}>
          Clear filters
        </Button>
      </div>
    )
  }

  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {rows.map((row) => (
        <AgentCard key={row.id} row={row} />
      ))}
    </div>
  )
}

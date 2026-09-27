import { BotIcon, PencilIcon, ShieldOffIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useAgentActivityParams } from "@/hooks/use-agent-activity-params"
import type { MyAgentActivitySummaryQuery } from "@/graphql/gql/graphql"

type AgentSummary = MyAgentActivitySummaryQuery["myAgentActivitySummary"][number]

interface AgentActivityToolbarProps {
  // Only to resolve a filtered key's id back to its name. The per-agent cards on
  // the Agents tab are where agents are chosen; here one is only ever narrowed
  // away from.
  agents: AgentSummary[]
  // Derived from the rows on screen: the only operations worth offering are
  // the ones an agent has actually touched.
  operations: { id: string; name: string }[]
  totalCount: number
}

/**
 * Filters over the audit feed.
 *
 * The two toggles are the two questions an operator asks. "Changes only"
 * answers what an agent did to their engagements. "Refused" answers where one
 * hit the edge of its key — usually meaning the key is scoped tighter than the
 * work, which is a configuration answer rather than a fault.
 *
 * It used to also render a chip per agent, carrying counts and last-seen. The
 * Agents tab presents that now, with the scope beside it, so what is left here
 * is one dismissible chip saying which agent the feed is narrowed to.
 */
export function AgentActivityToolbar({
  agents,
  operations,
  totalCount,
}: AgentActivityToolbarProps) {
  const {
    filters,
    filtered,
    setAgentKeyId,
    setOperationId,
    toggleWritesOnly,
    toggleRefusedOnly,
    clearFilters,
  } = useAgentActivityParams()

  const agentName =
    filters.agentKeyId == null
      ? null
      : agents.find((a) => a.agentKeyId === filters.agentKeyId)?.agentName ??
        // Linked to directly, or the key has been deleted since. The filter is
        // still doing something, so say so rather than dropping the chip.
        "this agent"

  return (
    <div className="flex flex-wrap items-center gap-2">
      {agentName && (
        <Button size="sm" variant="secondary" onClick={() => setAgentKeyId(null)}>
          <BotIcon className="size-3.5" />
          <span className="max-w-48 truncate">{agentName}</span>
          <XIcon className="size-3.5" />
        </Button>
      )}

      <Button
        size="sm"
        variant={filters.writesOnly ? "default" : "outline"}
        onClick={toggleWritesOnly}
      >
        <PencilIcon className="size-3.5" />
        Changes only
      </Button>
      <Button
        size="sm"
        variant={filters.refusedOnly ? "default" : "outline"}
        onClick={toggleRefusedOnly}
      >
        <ShieldOffIcon className="size-3.5" />
        Refused
      </Button>

      {operations.length > 1 &&
        operations.map((op) => (
          <Button
            key={op.id}
            size="sm"
            variant={filters.operationId === op.id ? "default" : "outline"}
            className="max-w-48 truncate"
            onClick={() =>
              setOperationId(filters.operationId === op.id ? null : op.id)
            }
          >
            {op.name}
          </Button>
        ))}

      {filtered && (
        <Button size="sm" variant="ghost" onClick={clearFilters}>
          Clear
        </Button>
      )}

      <span className="ml-auto text-xs text-muted-foreground">
        {totalCount} {totalCount === 1 ? "call" : "calls"}
      </span>
    </div>
  )
}

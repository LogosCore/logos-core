import { PlusIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { SearchInput } from "@/components/ui/search-input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ConnectAgentSheet } from "@/components/agents/connect-agent-sheet"
import { SkillDownloadButton } from "@/components/agents/skill-download-button"
import { useAgentStore } from "@/stores/agents"
import type { DataTableSort } from "@/lib/data-table-sort"
import type {
  AgentCounts,
  AgentSortField,
  AgentStatusFilter,
} from "@/lib/agent-rows"

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: "all", label: "All agents" },
  { value: "active", label: "Active" },
  { value: "paused", label: "Paused" },
  { value: "writable", label: "Can write" },
  { value: "readonly", label: "Read only" },
  { value: "unused", label: "Never used" },
]

// A grid has no column headers to click, so sort is a named choice rather than a
// field-plus-direction pair the operator has to assemble.
const SORT_OPTIONS: {
  value: string
  label: string
  sort: DataTableSort<AgentSortField>
}[] = [
  {
    value: "recent",
    label: "Recently active",
    sort: { field: "LAST_SEEN", direction: "DESC" },
  },
  { value: "name", label: "Name", sort: { field: "NAME", direction: "ASC" } },
  {
    value: "newest",
    label: "Newest",
    sort: { field: "CREATED", direction: "DESC" },
  },
]

interface AgentsToolbarProps {
  counts: AgentCounts
}

export function AgentsToolbar({ counts }: AgentsToolbarProps) {
  const search = useAgentStore((s) => s.search)
  const setSearch = useAgentStore((s) => s.setSearch)
  const statusFilter = useAgentStore((s) => s.statusFilter)
  const setStatusFilter = useAgentStore((s) => s.setStatusFilter)
  const sort = useAgentStore((s) => s.sort)
  const setSort = useAgentStore((s) => s.setSort)
  const openCreateForm = useAgentStore((s) => s.openCreateForm)

  const sortValue =
    SORT_OPTIONS.find(
      (o) => o.sort.field === sort.field && o.sort.direction === sort.direction,
    )?.value ?? "recent"

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SearchInput
          value={search}
          onValueChange={setSearch}
          placeholder="Search agents..."
        />
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={statusFilter ?? "all"}
            onValueChange={(value) =>
              setStatusFilter(value === "all" ? null : (value as AgentStatusFilter))
            }
          >
            <SelectTrigger className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUS_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select
            value={sortValue}
            onValueChange={(value) => {
              const option = SORT_OPTIONS.find((o) => o.value === value)
              if (option) setSort(option.sort)
            }}
          >
            <SelectTrigger className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SORT_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <SkillDownloadButton />
          <ConnectAgentSheet />
          <Button onClick={openCreateForm}>
            <PlusIcon className="size-4" />
            New agent
          </Button>
        </div>
      </div>

      <StateCounts counts={counts} />
    </div>
  )
}

/**
 * The states worth acting on, as text rather than as another row of controls.
 * Paused and never-used are the two an operator running many agents wants
 * pointed out, and both are already filterable above.
 */
function StateCounts({ counts }: AgentsToolbarProps) {
  if (counts.total === 0) return null

  return (
    <div className="text-xs text-muted-foreground">
      {counts.total} {counts.total === 1 ? "agent" : "agents"}
      {counts.paused > 0 && ` · ${counts.paused} paused`}
      {counts.unused > 0 && ` · ${counts.unused} never used`}
    </div>
  )
}

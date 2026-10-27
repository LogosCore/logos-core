import { SearchInput } from "@/components/ui/search-input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useModuleStore, type ModuleStatusFilter, type ModuleTypeFilter } from "@/stores/modules"

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: "all", label: "All states" },
  { value: "registered", label: "Registered" },
  { value: "deregistered", label: "Deregistered" },
  { value: "dead", label: "Dead" },
  { value: "blocked", label: "Blocked" },
]

const TYPE_OPTIONS: { value: string; label: string }[] = [
  { value: "all", label: "All types" },
  { value: "channel", label: "Channels" },
  { value: "factory", label: "Factories" },
]

/** Search and filters, as on the other lists: the server applies them. */
export function ModulesToolbar() {
  const search = useModuleStore((s) => s.search)
  const setSearch = useModuleStore((s) => s.setSearch)
  const statusFilter = useModuleStore((s) => s.statusFilter)
  const setStatusFilter = useModuleStore((s) => s.setStatusFilter)
  const typeFilter = useModuleStore((s) => s.typeFilter)
  const setTypeFilter = useModuleStore((s) => s.setTypeFilter)

  return (
    <div className="flex flex-wrap items-center gap-2">
      <SearchInput
        value={search}
        onValueChange={setSearch}
        placeholder="Search modules..."
      />
      <Select
        value={typeFilter ?? "all"}
        onValueChange={(val) => setTypeFilter(val === "all" ? null : (val as ModuleTypeFilter))}
      >
        <SelectTrigger className="w-40" aria-label="Type">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {TYPE_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        value={statusFilter ?? "all"}
        onValueChange={(val) =>
          setStatusFilter(val === "all" ? null : (val as ModuleStatusFilter))
        }
      >
        <SelectTrigger className="w-40" aria-label="State">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {STATUS_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

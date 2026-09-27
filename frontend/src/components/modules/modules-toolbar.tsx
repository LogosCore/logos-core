import { SearchInput } from "@/components/ui/search-input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useModuleStore, type ModuleStatusFilter } from "@/stores/modules"

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: "all", label: "All states" },
  { value: "registered", label: "Registered" },
  { value: "deregistered", label: "Deregistered" },
  { value: "dead", label: "Dead" },
]

export function ModulesToolbar() {
  const search = useModuleStore((s) => s.search)
  const setSearch = useModuleStore((s) => s.setSearch)
  const statusFilter = useModuleStore((s) => s.statusFilter)
  const setStatusFilter = useModuleStore((s) => s.setStatusFilter)

  return (
    <div className="flex items-center justify-between gap-3">
      <SearchInput
        value={search}
        onValueChange={setSearch}
        placeholder="Search modules..."
      />
      <Select
        value={statusFilter ?? "all"}
        onValueChange={(val) =>
          setStatusFilter(val === "all" ? null : (val as ModuleStatusFilter))
        }
      >
        <SelectTrigger className="w-40">
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

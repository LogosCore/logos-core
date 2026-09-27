import { create } from "zustand"
import type { DataTableSort } from "@/lib/data-table-sort"
import type { AgentSortField, AgentStatusFilter } from "@/lib/agent-rows"

// View state for the Agents page. Mirrors the skills store: search, a status
// filter and a sort, none of it persisted.
//
// The form target replaces what used to be a boolean "create form is open" —
// the same sheet now serves create and edit, and it carries which agent is
// being edited so the grid stays on screen behind it.
type AgentFormTarget = { mode: "create" } | { mode: "edit"; id: string }

interface AgentStoreState {
  search: string
  statusFilter: AgentStatusFilter
  sort: DataTableSort<AgentSortField>

  formTarget: AgentFormTarget | null

  // The raw lga_... token from the last create/regenerate, and which key it
  // belongs to. Null when there is nothing fresh to display. Rendered at page
  // level rather than inside a card: it is shown exactly once, and a banner
  // that can be scrolled out of a list is a banner that can be missed.
  freshToken: string | null
  freshTokenKeyId: string | null

  setSearch: (search: string) => void
  setStatusFilter: (status: AgentStatusFilter) => void
  setSort: (sort: DataTableSort<AgentSortField>) => void
  openCreateForm: () => void
  openEditForm: (id: string) => void
  closeForm: () => void
  setFreshToken: (token: string | null, keyId?: string | null) => void
  reset: () => void
}

const defaults = {
  search: "",
  statusFilter: null,
  // Newest activity first: the reason to open this page with several agents on
  // it is usually to see which one is working.
  sort: { field: "LAST_SEEN", direction: "DESC" } as DataTableSort<AgentSortField>,
  formTarget: null,
  freshToken: null,
  freshTokenKeyId: null,
}

export const useAgentStore = create<AgentStoreState>((set) => ({
  ...defaults,

  setSearch: (search) => set({ search }),
  setStatusFilter: (statusFilter) => set({ statusFilter }),
  setSort: (sort) => set({ sort }),
  openCreateForm: () => set({ formTarget: { mode: "create" } }),
  openEditForm: (id) => set({ formTarget: { mode: "edit", id } }),
  closeForm: () => set({ formTarget: null }),
  setFreshToken: (token, keyId = null) =>
    set({ freshToken: token, freshTokenKeyId: token ? keyId : null }),
  // Called when the page unmounts. Leaving a secret in memory across visits
  // would let it reappear long after the operator moved on.
  reset: () => set(defaults),
}))

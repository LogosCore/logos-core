import { create } from "zustand"

interface SelectedModule {
  instance: string
  type: string
}

// Lifecycle status filter for the modules list. `null` means "all states" —
// the default, so the operator sees every row together.
export type ModuleStatusFilter =
  | null
  | "registered"
  | "deregistered"
  | "dead"
  | "blocked"

/** An operator's change that asks for confirmation first. */
export type ModuleDialog = { kind: "block" | "delete"; module: SelectedModule }

/** Module type filter for the modules list; `null` means all types. */
export type ModuleTypeFilter = null | "channel" | "factory"

interface ModuleStoreState {
  search: string
  statusFilter: ModuleStatusFilter
  typeFilter: ModuleTypeFilter
  dialog: ModuleDialog | null

  // Actions.
  setSearch: (search: string) => void
  setStatusFilter: (status: ModuleStatusFilter) => void
  setTypeFilter: (type: ModuleTypeFilter) => void
  openDialog: (dialog: ModuleDialog) => void
  closeDialog: () => void
}

export const useModuleStore = create<ModuleStoreState>((set) => ({
  search: "",
  statusFilter: null,
  typeFilter: null,
  dialog: null,

  setSearch: (search) => set({ search }),
  setStatusFilter: (statusFilter) => set({ statusFilter }),
  setTypeFilter: (typeFilter) => set({ typeFilter }),
  openDialog: (dialog) => set({ dialog }),
  closeDialog: () => set({ dialog: null }),
}))

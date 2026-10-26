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

interface ModuleStoreState {
  search: string
  statusFilter: ModuleStatusFilter
  dialog: ModuleDialog | null

  // Actions.
  setSearch: (search: string) => void
  setStatusFilter: (status: ModuleStatusFilter) => void
  openDialog: (dialog: ModuleDialog) => void
  closeDialog: () => void
}

export const useModuleStore = create<ModuleStoreState>((set) => ({
  search: "",
  statusFilter: null,
  dialog: null,

  setSearch: (search) => set({ search }),
  setStatusFilter: (statusFilter) => set({ statusFilter }),
  openDialog: (dialog) => set({ dialog }),
  closeDialog: () => set({ dialog: null }),
}))

import { create } from "zustand"

// The Builds page's list filters, kept across visits like the other lists'.
// The server applies all of them (see the builds query). `null` means all.
interface BuildStoreState {
  search: string
  stateFilter: string | null
  factoryFilter: string | null

  setSearch: (search: string) => void
  setStateFilter: (state: string | null) => void
  setFactoryFilter: (factory: string | null) => void
}

export const useBuildStore = create<BuildStoreState>((set) => ({
  search: "",
  stateFilter: null,
  factoryFilter: null,

  setSearch: (search) => set({ search }),
  setStateFilter: (stateFilter) => set({ stateFilter }),
  setFactoryFilter: (factoryFilter) => set({ factoryFilter }),
}))

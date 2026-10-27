import { create } from "zustand"

// The Endpoints page's list filters, kept across visits like the other lists'.
// The server applies them (see the channelEndpoints query). `null` means all.
interface EndpointStoreState {
  search: string
  kindFilter: string | null

  setSearch: (search: string) => void
  setKindFilter: (kind: string | null) => void
}

export const useEndpointStore = create<EndpointStoreState>((set) => ({
  search: "",
  kindFilter: null,

  setSearch: (search) => set({ search }),
  setKindFilter: (kindFilter) => set({ kindFilter }),
}))

import { create } from "zustand"

// The Sessions page's search, kept across visits like the other lists'. The
// server applies it (see the minionSessions query).
interface MinionSessionStoreState {
  search: string
  setSearch: (search: string) => void
}

export const useMinionSessionStore = create<MinionSessionStoreState>((set) => ({
  search: "",
  setSearch: (search) => set({ search }),
}))

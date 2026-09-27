// The wiki palette's state and imperative entry points, deliberately kept in a
// module of their own, apart from the UI in wiki-command-palette.tsx.
//
// The split is a bundling constraint, not a style choice. The palette renders
// DocumentIcon, which reaches both icon catalogs — lucide's 1,940-entry glob
// thunk table and Simple Icons' 3,453-entry one, about 1 MB of JavaScript
// between them. Seven surfaces open the palette imperatively (the tree header
// and row menu, the /doc slash command, the move dialog, the import dialog,
// the task edit dialog's wiki references, and the wiki page's Cmd+K), and
// while `openWikiSearch` lived beside the component every one of those imports
// pulled that megabyte into its own chunk. AppLayout mounts the palette, so it
// landed in the app shell's static graph and every first paint — the login
// page included — paid to parse it.
//
// Importing from here costs a zustand store and a few type declarations. The
// component is loaded by wiki-palette-mount.tsx the first time the palette
// actually opens. Keep this module free of UI imports.

import { create } from "zustand"

// Shape passed back to the caller on pick. A subset of what `useWikiSearch`
// projects per hit — enough for typical post-pick work (insert a wiki
// reference node, add a relation, render a chip).
export interface PickedWikiDocument {
  id: string
  title: string
  emoji: string
  icon: string
  color: string
  /** A drawing renders a fixed glyph rather than the stored icon. */
  kind?: string | null
}

export interface PaletteScope {
  parentDocumentId: string | null
  parentTitle: string
}

// The palette runs in one of two modes. `navigate` is the Cmd+K / "search
// within X" surface that opens the chosen doc. `pick` is the imperative
// document picker that every reference surface (the /doc slash command, the
// move dialog's parent chooser, the task edit dialog's wiki references) opens
// via openWikiDocumentPicker — it hands the chosen doc back through onPick and
// never navigates.
export interface NavigateConfig {
  mode: "navigate"
  operationId: string
  scope: PaletteScope
}

export interface PickConfig {
  mode: "pick"
  operationId: string
  excludeIds: string[]
  title: string
  description: string
  onPick: (doc: PickedWikiDocument) => void
}

export type PaletteConfig = NavigateConfig | PickConfig

export interface OpenSearchArgs {
  operationId: string
  parentDocumentId: string | null
  parentTitle: string
}

export interface OpenPickArgs {
  operationId: string
  /** Document IDs that should appear muted and reject selection (e.g. the
   *  current doc when called from the /doc slash command, already-added
   *  references in the task dialog, or the moved doc + its descendants in the
   *  move dialog). */
  excludeIds?: string[]
  /** Override for the header label — defaults to "Insert document reference". */
  title?: string
  /** Optional context line shown under the search box. */
  description?: string
  onPick: (doc: PickedWikiDocument) => void
}

interface PaletteStore {
  config: PaletteConfig | null
  openNavigate: (args: OpenSearchArgs) => void
  openPick: (args: OpenPickArgs) => void
  close: () => void
}

// Singleton store — the palette is mounted once in AppLayout and any surface
// (tree search, /doc slash command, move dialog, task edit dialog) drives it
// imperatively. Replaces the old wiki-store `searchScope` field and the
// separate wiki-document-picker dialog/store; both search surfaces now share
// this one component and the ranked `wikiSearch` backend.
export const usePaletteStore = create<PaletteStore>((set) => ({
  config: null,
  openNavigate: ({ operationId, parentDocumentId, parentTitle }) =>
    set({
      config: {
        mode: "navigate",
        operationId,
        scope: { parentDocumentId, parentTitle },
      },
    }),
  openPick: ({ operationId, excludeIds, title, description, onPick }) =>
    set({
      config: {
        mode: "pick",
        operationId,
        excludeIds: excludeIds ?? [],
        title: title ?? "Insert document reference",
        description: description ?? "",
        onPick,
      },
    }),
  close: () => set({ config: null }),
}))

/** Open the palette in navigate mode (Cmd+K / "search within"). */
export function openWikiSearch(args: OpenSearchArgs) {
  usePaletteStore.getState().openNavigate(args)
}

/** Open the palette in pick mode — same imperative entry point every
 *  reference surface used before the unification. */
export function openWikiDocumentPicker(args: OpenPickArgs) {
  usePaletteStore.getState().openPick(args)
}

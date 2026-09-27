import { useCallback } from "react"
import { create } from "zustand"

/** The three sections of the wiki page footer, in render order. */
export type WikiFooterSectionId = "subpages" | "backlinks" | "tasks"

/** Which kind of page the footer sits under. */
export type WikiFooterSurface = "document" | "drawing"

const SECTION_IDS: readonly WikiFooterSectionId[] = [
  "subpages",
  "backlinks",
  "tasks",
]

const SURFACES: readonly WikiFooterSurface[] = ["document", "drawing"]

// A prose page's footer is the tail of a single scrolling column, so its lists
// open and the reader scrolls past them. A drawing's footer is a capped strip
// wedged under a canvas that owns the wheel: three open lists there cost the
// drawing the bottom of its own page, for links the operator did not come to
// the canvas to read. So a drawing starts folded, one click per list.
const DEFAULT_OPEN: Record<WikiFooterSurface, boolean> = {
  document: true,
  drawing: false,
}

const STORAGE_KEY = "wiki_footer_sections"

/** `document:backlinks` — one entry per surface × section. */
type SectionKey = `${WikiFooterSurface}:${WikiFooterSectionId}`

function sectionKey(
  surface: WikiFooterSurface,
  id: WikiFooterSectionId,
): SectionKey {
  return `${surface}:${id}`
}

const VALID_KEYS: ReadonlySet<string> = new Set(
  SURFACES.flatMap((surface) => SECTION_IDS.map((id) => sectionKey(surface, id))),
)

// Collapse is a per-viewer reading preference, not document state: it belongs
// to the operator across every page they open, so it is keyed by surface and
// section rather than per document or per operation, and never goes near Yjs —
// folding a section on my screen must not fold it on a collaborator's.
//
// Only sections the operator has actually toggled are stored, and an entry is
// dropped once it matches its surface's default again. That keeps DEFAULT_OPEN
// authoritative for everything untouched: the drawing defaults above can be
// changed, or a fourth section added, with nothing to migrate.
type Overrides = Readonly<Partial<Record<SectionKey, boolean>>>

function loadOverrides(): Overrides {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return {}
    }
    const overrides: Partial<Record<SectionKey, boolean>> = {}
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === "boolean" && VALID_KEYS.has(key)) {
        overrides[key as SectionKey] = value
      }
    }
    return overrides
  } catch {
    // Corrupt or unavailable storage — every section falls back to its
    // surface's default, which is the state the footer ships with.
    return {}
  }
}

function saveOverrides(overrides: Overrides) {
  try {
    if (Object.keys(overrides).length === 0) localStorage.removeItem(STORAGE_KEY)
    else localStorage.setItem(STORAGE_KEY, JSON.stringify(overrides))
  } catch {
    // Storage full or disabled only costs the preference its memory; the
    // in-memory state still drives this session.
  }
}

interface WikiFooterSectionsState {
  overrides: Overrides
  /**
   * Absolute setter rather than a toggle, so it can be handed straight to a
   * Collapsible's `onOpenChange`. Returning the identical state when nothing
   * changes keeps a redundant call from re-rendering subscribers.
   */
  setOpen: (
    surface: WikiFooterSurface,
    id: WikiFooterSectionId,
    open: boolean,
  ) => void
}

export const useWikiFooterSectionsStore = create<WikiFooterSectionsState>(
  (set) => ({
    overrides: loadOverrides(),

    setOpen: (surface, id, open) =>
      set((state) => {
        const key = sectionKey(surface, id)
        if (isSectionOpen(state.overrides, surface, id) === open) return state
        const next = { ...state.overrides }
        if (open === DEFAULT_OPEN[surface]) delete next[key]
        else next[key] = open
        saveOverrides(next)
        return { overrides: next }
      }),
  }),
)

/** Whether one section shows, given the stored overrides. */
export function isSectionOpen(
  overrides: Overrides,
  surface: WikiFooterSurface,
  id: WikiFooterSectionId,
): boolean {
  return overrides[sectionKey(surface, id)] ?? DEFAULT_OPEN[surface]
}

/** `[open, setOpen]` for one footer section, ready for a CollapsibleSection. */
export function useWikiFooterSection(
  surface: WikiFooterSurface,
  id: WikiFooterSectionId,
): [boolean, (open: boolean) => void] {
  const open = useWikiFooterSectionsStore((s) =>
    isSectionOpen(s.overrides, surface, id),
  )
  const setOpen = useWikiFooterSectionsStore((s) => s.setOpen)
  return [
    open,
    useCallback((next: boolean) => setOpen(surface, id, next), [surface, id, setOpen]),
  ]
}

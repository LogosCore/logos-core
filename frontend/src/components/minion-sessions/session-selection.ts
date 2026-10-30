// The Sessions table's selection, for bulk actions: the rows picked, or all
// the sessions the list matches (its view and filters, every page). It
// belongs to one scope (the view and the filters): a selection made under
// another is empty, so changing them clears it, and nobody acts on rows they
// cannot see. Rows that leave the list (archived elsewhere, restored by a
// check-in) drop out of it.

export interface SessionSelection {
  /** The view and filters it was made under. */
  scope: string
  /** The rows picked; with all, ignored. */
  ids: string[]
  /** Every session the list matches, loaded or not. */
  all: boolean
  /** The last row clicked: where a shift-click's range starts. */
  anchor: string | null
}

export function emptySelection(scope: string): SessionSelection {
  return { scope, ids: [], all: false, anchor: null }
}

/** The selection as it stands for scope: empty when made under another. */
export function selectionIn(sel: SessionSelection, scope: string): SessionSelection {
  return sel.scope === scope ? sel : emptySelection(scope)
}

/** The loaded rows selected, in the list's order. */
export function selectedIds(sel: SessionSelection, order: readonly string[]): string[] {
  if (sel.all) return [...order]
  const picked = new Set(sel.ids)
  return order.filter((id) => picked.has(id))
}

/**
 * A click on a row: toggles it, or with shift selects every row from the
 * last one clicked to it. With no row clicked yet (picked by the header
 * checkbox or "all matching", or the last one gone from the list), the range
 * starts at the nearest picked row. Clicking under "all matching" goes back
 * to picked rows: the loaded ones, less (or plus) what the click says.
 */
export function toggleSelected(sel: SessionSelection, order: readonly string[], id: string, shift: boolean): SessionSelection {
  const base = sel.all ? [...order] : sel.ids
  let ids: string[]
  const to = order.indexOf(id)
  let from = sel.anchor === null ? -1 : order.indexOf(sel.anchor)
  if (shift && from < 0 && to >= 0) from = nearestPicked(order, new Set(base), to)
  if (shift && from >= 0 && to >= 0) {
    const range = order.slice(Math.min(from, to), Math.max(from, to) + 1)
    ids = [...new Set([...base, ...range])]
  } else {
    ids = base.includes(id) ? base.filter((x) => x !== id) : [...base, id]
  }
  return { ...sel, ids, all: false, anchor: id }
}

/** The header checkbox: every loaded row, or none. */
export function selectLoaded(sel: SessionSelection, order: readonly string[], on: boolean): SessionSelection {
  return { ...sel, ids: on ? [...order] : [], all: false, anchor: null }
}

/** The index of the picked row nearest to index at (not itself), or -1. */
function nearestPicked(order: readonly string[], picked: ReadonlySet<string>, at: number): number {
  for (let d = 1; d < order.length; d++) {
    if (at - d >= 0 && picked.has(order[at - d])) return at - d
    if (at + d < order.length && picked.has(order[at + d])) return at + d
  }
  return -1
}

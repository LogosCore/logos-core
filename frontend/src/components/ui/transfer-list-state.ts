// The transfer list's logic (transfer-list.tsx) that needs no page: which
// items each list shows, moving items between the lists, and marking them
// with the mouse and the keyboard. Apart from the component so it can be
// tested on its own.

/** One item of a transfer list. */
export interface TransferItem {
  /** Tells items apart; what the selection holds. */
  id: string
  label: string
  /** Beside the label, muted and monospaced (an id, say). */
  hint?: string
  /** In the item's tooltip, and searched. */
  description?: string | null
  /** The heading the item is listed under. */
  group?: string
  /** Selected for good: it never leaves the selected list. */
  locked?: boolean
}

export type Side = "available" | "selected"

/** Whether item matches a search: its label, hint or description holds query, case aside. */
export function matches(item: TransferItem, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (q === "") return true
  return [item.label, item.hint ?? "", item.description ?? ""].some((text) => text.toLowerCase().includes(q))
}

/** The ids selected, locked ones always among them, in the items' order. */
export function normalized(items: readonly TransferItem[], selected: Iterable<string>): string[] {
  const on = new Set(selected)
  return items.filter((i) => i.locked || on.has(i.id)).map((i) => i.id)
}

/** The items a list shows: those on its side that match query, in the items' order. */
export function sideItems(items: readonly TransferItem[], selected: readonly string[], side: Side, query: string): TransferItem[] {
  const on = new Set(normalized(items, selected))
  return items.filter((i) => on.has(i.id) === (side === "selected") && matches(i, query))
}

/** The selection with ids moved to side: added to it, or taken out of it, locked ones staying. */
export function moved(items: readonly TransferItem[], selected: readonly string[], ids: Iterable<string>, to: Side): string[] {
  const moving = new Set(ids)
  const on = new Set(selected)
  for (const id of moving) {
    if (to === "selected") on.add(id)
    else on.delete(id)
  }
  return normalized(items, on)
}

/** Whether an item can leave the side it is on: a locked one cannot leave the selected list. */
export function movable(item: TransferItem, side: Side): boolean {
  return side === "available" || !item.locked
}

/** Items under their headings, in their order: the first item of each heading places it. */
export function grouped(items: readonly TransferItem[]): { group: string; items: TransferItem[] }[] {
  const groups = new Map<string, TransferItem[]>()
  for (const item of items) {
    const g = item.group ?? ""
    const list = groups.get(g)
    if (list) list.push(item)
    else groups.set(g, [item])
  }
  return [...groups].map(([group, items]) => ({ group, items }))
}

/** What is marked in one list, and where a range marked with Shift starts. */
export interface Marks {
  marked: ReadonlySet<string>
  anchor: string | null
}

export const NO_MARKS: Marks = { marked: new Set(), anchor: null }

/**
 * The marks after a click on id, of the ids a list shows: alone; added or
 * taken away with Ctrl or ⌘ (toggle); or, with Shift, every one from the
 * anchor to it.
 */
export function clicked(shown: readonly string[], marks: Marks, id: string, mods: { shift: boolean; toggle: boolean }): Marks {
  if (mods.shift && marks.anchor !== null && shown.includes(marks.anchor)) {
    return { marked: new Set(span(shown, marks.anchor, id)), anchor: marks.anchor }
  }
  if (mods.toggle) {
    const marked = new Set(marks.marked)
    if (marked.has(id)) marked.delete(id)
    else marked.add(id)
    return { marked, anchor: id }
  }
  return { marked: new Set([id]), anchor: id }
}

/** The ids from a to b in shown, either way round. */
function span(shown: readonly string[], a: string, b: string): string[] {
  const i = shown.indexOf(a)
  const j = shown.indexOf(b)
  if (i < 0 || j < 0) return [b]
  return shown.slice(Math.min(i, j), Math.max(i, j) + 1)
}

/**
 * The item a list's keyboard focus goes to on a key, of the ids it shows:
 * the next or previous one, the first or the last. Null when it shows none.
 */
export function stepped(shown: readonly string[], active: string | null, key: "next" | "previous" | "first" | "last"): string | null {
  if (shown.length === 0) return null
  const at = active === null ? -1 : shown.indexOf(active)
  switch (key) {
    case "first":
      return shown[0]
    case "last":
      return shown[shown.length - 1]
    case "next":
      return shown[at < 0 ? 0 : Math.min(at + 1, shown.length - 1)]
    case "previous":
      return shown[at < 0 ? 0 : Math.max(at - 1, 0)]
  }
}

/**
 * The marks after the keyboard focus moved to id: with Shift, every one from
 * the anchor (the item focus left, when none) to it; otherwise as they were.
 */
export function extended(shown: readonly string[], marks: Marks, from: string | null, id: string, shift: boolean): Marks {
  if (!shift) return marks
  const anchor = marks.anchor !== null && shown.includes(marks.anchor) ? marks.anchor : (from ?? id)
  return { marked: new Set(span(shown, anchor, id)), anchor }
}

/** The ids to move from a list: its marked ones it shows and can let go, or else the focused one. */
export function leaving(shown: readonly TransferItem[], side: Side, marks: Marks, active: string | null): string[] {
  const free = shown.filter((i) => movable(i, side))
  const marked = free.filter((i) => marks.marked.has(i.id)).map((i) => i.id)
  if (marked.length > 0) return marked
  return free.some((i) => i.id === active) ? [active!] : []
}

/** Where focus goes in a list after ids left it: the item after the first one gone, else the one before. */
export function after(shown: readonly string[], gone: readonly string[]): string | null {
  const left = new Set(gone)
  const first = shown.findIndex((id) => left.has(id))
  if (first < 0) return null
  return shown.slice(first).find((id) => !left.has(id)) ?? [...shown.slice(0, first)].reverse().find((id) => !left.has(id)) ?? null
}

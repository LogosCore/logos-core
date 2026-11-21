// Highlighted fields of the module logs (logs-tab.tsx): an operator picks a
// field of a line (its path, "headers.accept", from the detail pane), and
// every line shows that field's value up front, in the field's tone, rather
// than somewhere in its muted key=value tail. A few at once, each its own
// tone, the same in the toolbar's chips, the lines and the detail pane.

import type { AttrPair } from "./log-buffer"

/**
 * Each highlighted field's tone, by its place among them: a chip's (a
 * value in a line, a field in the toolbar), and an icon's. None of the
 * levels' (sky, amber, red), so a value never reads as a level.
 */
export const HIGHLIGHT_TONES = [
  { chip: "bg-violet-500/15 text-violet-800 dark:text-violet-300", icon: "text-violet-600 dark:text-violet-400" },
  { chip: "bg-emerald-500/15 text-emerald-800 dark:text-emerald-300", icon: "text-emerald-600 dark:text-emerald-400" },
  { chip: "bg-fuchsia-500/15 text-fuchsia-800 dark:text-fuchsia-300", icon: "text-fuchsia-600 dark:text-fuchsia-400" },
  { chip: "bg-orange-500/15 text-orange-800 dark:text-orange-300", icon: "text-orange-600 dark:text-orange-400" },
  { chip: "bg-cyan-500/15 text-cyan-800 dark:text-cyan-300", icon: "text-cyan-600 dark:text-cyan-400" },
] as const

/** The most fields highlighted at once: as many as there are tones to tell them apart. */
export const MAX_HIGHLIGHTS = HIGHLIGHT_TONES.length

/** The highlights with path toggled: added last while there is room, or taken out. */
export function toggleHighlight(highlights: readonly string[], path: string): string[] {
  if (highlights.includes(path)) return highlights.filter((p) => p !== path)
  return highlights.length < MAX_HIGHLIGHTS ? [...highlights, path] : [...highlights]
}

/** A highlighted pair of a line, with its tone (its field's place among the highlights). */
export interface HighlightedPair extends AttrPair {
  tone: number
}

/**
 * A line's pairs split: the highlighted ones in the highlights' order, each
 * with its tone (a line without the field has none for it), and the rest in
 * the record's order.
 */
export function splitHighlighted(
  pairs: readonly AttrPair[],
  highlights: readonly string[],
): { picked: HighlightedPair[]; rest: AttrPair[] } {
  if (highlights.length === 0) return { picked: [], rest: [...pairs] }
  const picked: HighlightedPair[] = []
  highlights.forEach((path, tone) => {
    const p = pairs.find((x) => x.path === path)
    if (p) picked.push({ ...p, tone })
  })
  return { picked, rest: pairs.filter((p) => !highlights.includes(p.path)) }
}

/** What the highlights of a module are kept under: modules of a kind log the same fields. */
export function highlightsKey(module: { type: string; name: string }): string {
  return `${module.type}:${module.name}`
}

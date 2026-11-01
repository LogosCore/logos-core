import { createContext, useContext, type CSSProperties } from "react"

// The session work pane's text size (docs: its layout's paneTextSize): the
// operator's, from 12px (the app's text-xs, a terminal's density) up. The
// pane redefines Tailwind's text sizes for everything in it (paneTextStyle),
// so a view sizes its text as anywhere else; the few that lay rows out in
// pixels (a virtual list, the hex view) scale them (usePaneRow).

export const PANE_TEXT_SIZES: readonly number[] = [12, 13, 14]
export const DEFAULT_PANE_TEXT = 13

/** text-xs at the size, text-sm two pixels up, text-2xs one down. */
export function paneTextStyle(size: number): CSSProperties {
  return {
    "--text-xs": `${size}px`,
    "--text-sm": `${size + 2}px`,
    "--text-2xs": `${size - 1}px`,
  } as CSSProperties
}

/** The pane's text size; outside it, the app's 12px. */
export const PaneTextContext = createContext(12)

/** A row height laid out for 12px text, at the pane's size. */
export function usePaneRow(at12: number): number {
  return Math.round((at12 * useContext(PaneTextContext)) / 12)
}

/** The pane's text size. */
export function usePaneText(): number {
  return useContext(PaneTextContext)
}

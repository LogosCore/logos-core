import { createContext, useContext, type CSSProperties } from "react"

// The session work pane's text size (docs: its layout's paneTextSize): the
// operator's, 12px (the app's own, the sessions table's: text-xs for
// toolbars, headings and mono values, text-sm for rows) or smaller. The pane's
// tabs redefine Tailwind's two sizes for everything in them (paneTextStyle),
// so a view sizes its text as anywhere else; the few that lay rows out in
// pixels (a virtual list, the hex view) scale them (usePaneRow).

export const PANE_TEXT_SIZES: readonly number[] = [10, 11, 12]
export const DEFAULT_PANE_TEXT = 12

/** text-xs at the size, text-sm two pixels up: 12 and 14 by default, as the app's. */
export function paneTextStyle(size: number): CSSProperties {
  return {
    "--text-xs": `${size}px`,
    "--text-sm": `${size + 2}px`,
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

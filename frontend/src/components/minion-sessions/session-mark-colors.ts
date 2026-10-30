// The 36 colors an operator marks sessions with (core's SessionMarkColors):
// 12 hues in 3 strengths, "<hue>-<strength>". Each has a row tint and the
// tint on hover, and a tone for the mark's icon, in either theme. OKLCH keeps
// the strengths even across hues; the tints stay light (or, in the dark
// theme, low), so a marked row reads as marked without turning the table
// into a quilt, and the app's neutral look holds.

export type SessionMarkStrength = "soft" | "medium" | "strong"

export interface SessionMarkColor {
  id: string
  label: string
  hue: string
  strength: SessionMarkStrength
  /** Row background, light and dark theme, and on hover. */
  rowLight: string
  rowDark: string
  rowLightHover: string
  rowDarkHover: string
  /** The icon's tone, light and dark theme; also the swatch. */
  toneLight: string
  toneDark: string
}

const HUES: readonly { id: string; label: string; h: number }[] = [
  { id: "red", label: "Red", h: 25 },
  { id: "orange", label: "Orange", h: 50 },
  { id: "amber", label: "Amber", h: 75 },
  { id: "yellow", label: "Yellow", h: 95 },
  { id: "lime", label: "Lime", h: 125 },
  { id: "green", label: "Green", h: 150 },
  { id: "teal", label: "Teal", h: 180 },
  { id: "cyan", label: "Cyan", h: 205 },
  { id: "blue", label: "Blue", h: 250 },
  { id: "indigo", label: "Indigo", h: 275 },
  { id: "violet", label: "Violet", h: 300 },
  { id: "pink", label: "Pink", h: 350 },
]

// Per strength: the row's lightness and chroma in each theme, and the
// icon's chroma (its lightness stays put, so icons read alike).
const STRENGTHS: Record<SessionMarkStrength, { lightL: number; darkL: number; rowC: number; toneC: number }> = {
  soft: { lightL: 0.975, darkL: 0.24, rowC: 0.02, toneC: 0.1 },
  medium: { lightL: 0.95, darkL: 0.28, rowC: 0.045, toneC: 0.14 },
  strong: { lightL: 0.91, darkL: 0.33, rowC: 0.075, toneC: 0.18 },
}

const oklch = (l: number, c: number, h: number) => `oklch(${l} ${c} ${h})`

export const SESSION_MARK_COLORS: readonly SessionMarkColor[] = HUES.flatMap((hue) =>
  (Object.keys(STRENGTHS) as SessionMarkStrength[]).map((strength) => {
    const s = STRENGTHS[strength]
    return {
      id: `${hue.id}-${strength}`,
      label: `${hue.label}, ${strength}`,
      hue: hue.id,
      strength,
      rowLight: oklch(s.lightL, s.rowC, hue.h),
      rowDark: oklch(s.darkL, s.rowC, hue.h),
      rowLightHover: oklch(s.lightL - 0.03, s.rowC + 0.015, hue.h),
      rowDarkHover: oklch(s.darkL + 0.04, s.rowC + 0.015, hue.h),
      toneLight: oklch(0.55, s.toneC, hue.h),
      toneDark: oklch(0.78, s.toneC, hue.h),
    }
  }),
)

export const SESSION_MARK_COLOR_BY_ID: ReadonlyMap<string, SessionMarkColor> = new Map(
  SESSION_MARK_COLORS.map((c) => [c.id, c]),
)

/**
 * The CSS variables a marked row sets for its color (classes in
 * MARKED_ROW_CLASSES and MARK_TONE_CLASSES read them); none for no color.
 */
export function markColorVars(id: string | null | undefined): Record<string, string> {
  const c = id ? SESSION_MARK_COLOR_BY_ID.get(id) : undefined
  if (!c) return {}
  return {
    "--mark-row": c.rowLight,
    "--mark-row-dark": c.rowDark,
    "--mark-row-hover": c.rowLightHover,
    "--mark-row-dark-hover": c.rowDarkHover,
    "--mark-tone": c.toneLight,
    "--mark-tone-dark": c.toneDark,
  }
}

/** A marked row's tint, in either theme and on hover. */
export const MARKED_ROW_CLASSES =
  "bg-(color:--mark-row) hover:bg-(color:--mark-row-hover) dark:bg-(color:--mark-row-dark) dark:hover:bg-(color:--mark-row-dark-hover)"

/** Text (an icon) in the mark's tone, in either theme. */
export const MARK_TONE_CLASSES = "text-(color:--mark-tone) dark:text-(color:--mark-tone-dark)"

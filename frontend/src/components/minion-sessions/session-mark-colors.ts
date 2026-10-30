// The 36 colors an operator marks sessions with (core's SessionMarkColors):
// 12 hues in 3 strengths, "<hue>-<strength>". Each has a row tint and the
// tint on hover, and a tone for the mark's icon, in either theme. OKLCH keeps
// the strengths even across hues; the tints stay light (or, in the dark
// theme, low), so a marked row reads as marked without turning the table
// into a quilt, and the app's neutral look holds.

import type { LucideIcon } from "lucide-react"
import { CrownIcon, FlameIcon, GhostIcon, MonitorIcon, RainbowIcon, SquareTerminalIcon, SunsetIcon, ZapIcon } from "lucide-react"
import "./session-mark-styles.css"

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

/** The hues and strengths in the order the picker lays them out. */
export const SESSION_MARK_HUES: readonly string[] = HUES.map((h) => h.id)
export const SESSION_MARK_STRENGTHS = Object.keys(STRENGTHS) as SessionMarkStrength[]

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

/**
 * The special looks a mark gives a row in place of a color (core's
 * SessionMarkStyles; drawn by session-mark-styles.css). They share the mark's
 * color field. text says how the row's text is drawn there: "solid" one color,
 * "ink" a gradient through the text, "none" left as it is.
 */
export interface SessionMarkStyle {
  id: string
  label: string
  text: "solid" | "ink" | "none"
  /** The swatch, a square in the style's colors with its icon, per theme
   *  (backgrounds as images: some are gradients). */
  swatch: { light: string; dark: string; fg: string; fgDark: string }
  Icon: LucideIcon
}

export const SESSION_MARK_STYLES: readonly SessionMarkStyle[] = [
  {
    id: "haxor",
    label: "Haxor",
    text: "solid",
    Icon: SquareTerminalIcon,
    swatch: {
      light:
        "repeating-linear-gradient(0deg, rgb(0 60 20 / 0.06) 0 1px, transparent 1px 3px), linear-gradient(90deg, #39ff14 0 3px, oklch(0.94 0.05 140) 3px)",
      dark: "linear-gradient(#070b07, #070b07)",
      fg: "oklch(0.34 0.08 150)",
      fgDark: "#39ff14",
    },
  },
  {
    id: "rainbow",
    label: "Rainbow",
    text: "ink",
    Icon: RainbowIcon,
    swatch: {
      light: "conic-gradient(from 90deg, #ff4d4d, #ff9f1a, #ffe14d, #4dff88, #4dd2ff, #8f6bff, #ff4dd2, #ff4d4d)",
      dark: "conic-gradient(from 90deg, #ff4d4d, #ff9f1a, #ffe14d, #4dff88, #4dd2ff, #8f6bff, #ff4dd2, #ff4d4d)",
      fg: "#ffffff",
      fgDark: "#ffffff",
    },
  },
  {
    id: "amber-crt",
    label: "Amber CRT",
    text: "solid",
    Icon: MonitorIcon,
    swatch: {
      light:
        "repeating-linear-gradient(0deg, rgb(90 50 0 / 0.07) 0 1px, transparent 1px 3px), linear-gradient(90deg, #ffb000 0 3px, oklch(0.94 0.065 82) 3px)",
      dark: "linear-gradient(#160e02, #160e02)",
      fg: "oklch(0.37 0.08 55)",
      fgDark: "#ffb000",
    },
  },
  {
    id: "synthwave",
    label: "Synthwave",
    text: "solid",
    Icon: SunsetIcon,
    swatch: {
      light: "linear-gradient(135deg, oklch(0.94 0.045 340), oklch(0.93 0.05 300))",
      dark: "linear-gradient(135deg, #2a0d52, #9c1c63)",
      fg: "oklch(0.52 0.2 345)",
      fgDark: "#ffa6ea",
    },
  },
  {
    id: "glitch",
    label: "Glitch",
    text: "solid",
    Icon: ZapIcon,
    swatch: {
      light: "linear-gradient(oklch(0.96 0 0), oklch(0.96 0 0))",
      dark: "linear-gradient(#0b0b10, #0b0b10)",
      fg: "oklch(0.55 0.12 215)",
      fgDark: "#00f0ff",
    },
  },
  {
    id: "on-fire",
    label: "On Fire",
    text: "ink",
    Icon: FlameIcon,
    swatch: {
      light: "linear-gradient(oklch(0.95 0.04 50), oklch(0.95 0.04 50))",
      dark: "linear-gradient(180deg, #2c0b03, #1a0703)",
      fg: "#e04800",
      fgDark: "#ff7a00",
    },
  },
  {
    id: "gold",
    label: "Gold",
    text: "ink",
    Icon: CrownIcon,
    swatch: {
      light: "linear-gradient(oklch(0.95 0.045 90), oklch(0.95 0.045 90))",
      dark: "linear-gradient(135deg, #15110a, #2b220f)",
      fg: "#a9800f",
      fgDark: "#ffd700",
    },
  },
  {
    id: "ghost",
    label: "Ghost",
    text: "none",
    Icon: GhostIcon,
    swatch: {
      light: "linear-gradient(135deg, #e5e7eb, #9ca3af)",
      dark: "linear-gradient(135deg, #52525b, #27272a)",
      fg: "#ffffff",
      fgDark: "#d4d4d8",
    },
  },
]

export const SESSION_MARK_STYLE_BY_ID: ReadonlyMap<string, SessionMarkStyle> = new Map(
  SESSION_MARK_STYLES.map((s) => [s.id, s]),
)

/** Whether a mark's color field holds one of the colors (not a style). */
export function isMarkColor(id: string | null | undefined): boolean {
  return !!id && SESSION_MARK_COLOR_BY_ID.has(id)
}

/** The classes a row takes for its mark's color or style. */
export function markRowClass(id: string | null | undefined): string | undefined {
  if (!id) return undefined
  if (SESSION_MARK_COLOR_BY_ID.has(id)) return MARKED_ROW_CLASSES
  const style = SESSION_MARK_STYLE_BY_ID.get(id)
  if (!style) return undefined
  return `mark-fx mark-fx-${style.id}${style.text === "none" ? "" : ` mark-fx-${style.text}`}`
}

/** A marked row's tint, in either theme and on hover. */
export const MARKED_ROW_CLASSES =
  "bg-(color:--mark-row) hover:bg-(color:--mark-row-hover) dark:bg-(color:--mark-row-dark) dark:hover:bg-(color:--mark-row-dark-hover)"

/** Text (an icon) in the mark's tone, in either theme. */
export const MARK_TONE_CLASSES = "text-(color:--mark-tone) dark:text-(color:--mark-tone-dark)"

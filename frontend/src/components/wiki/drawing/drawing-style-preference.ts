// The style the next element is drawn with — Excalidraw's appState
// currentItem* fields: arrow type, colours, stroke, sloppiness, font, and so
// on — remembered per browser, like the grid (drawing-grid-preference.ts).
//
// Excalidraw keeps these for the session only, and the canvas starts every
// drawing from a fresh appState, so an operator who draws with elbow arrows
// had to pick them again on every page. They are the operator's, not the
// drawing's: the scene sync never shares them (drawing-scene.ts,
// SharedAppState).

import type { AppState } from "@excalidraw/excalidraw/types"

const STORAGE_KEY = "wiki_drawing_style"

export type DrawingStyle = Partial<
  Pick<
    AppState,
    | "currentItemArrowType"
    | "currentItemStrokeColor"
    | "currentItemBackgroundColor"
    | "currentItemFillStyle"
    | "currentItemStrokeWidth"
    | "currentItemStrokeStyle"
    | "currentItemRoughness"
    | "currentItemOpacity"
    | "currentItemFontFamily"
    | "currentItemFontSize"
    | "currentItemTextAlign"
    | "currentItemStartArrowhead"
    | "currentItemEndArrowhead"
    | "currentItemRoundness"
  >
>

const ARROWHEADS = [
  "arrow",
  "bar",
  "dot",
  "circle",
  "circle_outline",
  "triangle",
  "triangle_outline",
  "diamond",
  "diamond_outline",
  "crowfoot_one",
  "crowfoot_many",
  "crowfoot_one_or_many",
]

const oneOf =
  <T extends string>(...values: T[]) =>
  (v: unknown): v is T =>
    typeof v === "string" && (values as string[]).includes(v)
const numberIn =
  (min: number, max: number) =>
  (v: unknown): v is number =>
    typeof v === "number" && Number.isFinite(v) && v >= min && v <= max
const colour = (v: unknown): v is string => typeof v === "string" && v.length > 0 && v.length <= 64
const arrowhead = (v: unknown) => v === null || (typeof v === "string" && ARROWHEADS.includes(v))

/** Each field and what a stored value must be to be used. Storage is the
 * operator's own, but a value an older or newer Excalidraw wrote should be
 * dropped rather than handed to this one. */
const FIELDS: { [K in keyof DrawingStyle]-?: (v: unknown) => boolean } = {
  currentItemArrowType: oneOf("sharp", "round", "elbow"),
  currentItemStrokeColor: colour,
  currentItemBackgroundColor: colour,
  currentItemFillStyle: oneOf("hachure", "cross-hatch", "solid", "zigzag"),
  currentItemStrokeWidth: numberIn(0.5, 32),
  currentItemStrokeStyle: oneOf("solid", "dashed", "dotted"),
  currentItemRoughness: numberIn(0, 2),
  currentItemOpacity: numberIn(0, 100),
  currentItemFontFamily: (v) => typeof v === "number" && Number.isInteger(v) && v > 0 && v < 100,
  currentItemFontSize: numberIn(4, 400),
  currentItemTextAlign: oneOf("left", "center", "right"),
  currentItemStartArrowhead: arrowhead,
  currentItemEndArrowhead: arrowhead,
  currentItemRoundness: oneOf("sharp", "round"),
}

const KEYS = Object.keys(FIELDS) as (keyof DrawingStyle)[]

/** The style fields of an appState. */
export function styleOf(appState: Partial<AppState>): DrawingStyle {
  const out: Record<string, unknown> = {}
  for (const key of KEYS) {
    if (key in appState) out[key] = appState[key]
  }
  return out as DrawingStyle
}

/** The remembered style; fields not remembered, or not valid, are left out
 * and keep Excalidraw's defaults. */
export function readStylePreference(): DrawingStyle {
  let parsed: unknown
  try {
    parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null")
  } catch {
    return {}
  }
  if (!parsed || typeof parsed !== "object") return {}
  const out: Record<string, unknown> = {}
  for (const key of KEYS) {
    const value = (parsed as Record<string, unknown>)[key]
    if (value !== undefined && FIELDS[key](value)) out[key] = value
  }
  return out as DrawingStyle
}

export function writeStylePreference(style: DrawingStyle) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(style))
  } catch {
    // Storage unavailable: the next drawing starts from Excalidraw's defaults.
  }
}

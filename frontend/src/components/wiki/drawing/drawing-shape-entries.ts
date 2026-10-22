// What the Shapes panel and the quick picker (drawing-quick-shapes.tsx) both
// offer: the built-in shapes and the record cards, their previews, and the
// panel's preferences, shared so a style picked in one is the style of the
// other.

import { useCallback, useSyncExternalStore } from "react"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"

import { buildRefCard, type BuiltinShape, type RefKind, type ShapeStyle } from "./drawing-catalogue"
import { skeletonElements } from "./drawing-place"

// ---------------------------------------------------------------------------
// Preferences: style, collapsed categories, docked, and the shape last added
// from the quick picker. Per browser, like the grid preference.

const PREFS_KEY = "wiki_drawing_shapes_panel"
const PREFS_EVENT = "logos:shapes-prefs"

export interface ShapePrefs {
  style: ShapeStyle
  collapsed: string[]
  docked: boolean
  /** The quick picker's last choice: a shape key, or record:<kind>. */
  last?: string
}

let cached: { raw: string | null; prefs: ShapePrefs } | null = null

export function readPrefs(): ShapePrefs {
  let raw: string | null = null
  try {
    raw = localStorage.getItem(PREFS_KEY)
  } catch {
    // Storage unavailable: defaults.
  }
  // The same object while storage holds the same text, as
  // useSyncExternalStore needs.
  if (cached && cached.raw === raw) return cached.prefs
  let parsed: Partial<ShapePrefs> | null = null
  try {
    parsed = JSON.parse(raw ?? "null") as Partial<ShapePrefs> | null
  } catch {
    parsed = null
  }
  const prefs: ShapePrefs = {
    style: parsed?.style === "clean" ? "clean" : "sketch",
    collapsed: Array.isArray(parsed?.collapsed) ? parsed.collapsed.filter((c) => typeof c === "string") : [],
    docked: parsed?.docked === true,
    ...(typeof parsed?.last === "string" ? { last: parsed.last } : {}),
  }
  cached = { raw, prefs }
  return prefs
}

export function writePrefs(next: Partial<ShapePrefs>) {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ ...readPrefs(), ...next }))
  } catch {
    // Storage unavailable: the panel starts from its defaults next time.
  }
  window.dispatchEvent(new Event(PREFS_EVENT))
}

function subscribe(onChange: () => void) {
  window.addEventListener(PREFS_EVENT, onChange)
  window.addEventListener("storage", onChange)
  return () => {
    window.removeEventListener(PREFS_EVENT, onChange)
    window.removeEventListener("storage", onChange)
  }
}

/** The preferences, live: a change in the panel or the picker shows in both. */
export function useShapePrefs(): [ShapePrefs, (next: Partial<ShapePrefs>) => void] {
  const prefs = useSyncExternalStore(subscribe, readPrefs, readPrefs)
  return [prefs, useCallback((next: Partial<ShapePrefs>) => writePrefs(next), [])]
}

// ---------------------------------------------------------------------------
// Elements

/** A shape's elements, converted from its skeleton. Built fresh for every
 * insert, after the canvas has loaded its fonts. */
export function buildShape(shape: BuiltinShape, style: ShapeStyle): ExcalidrawElement[] {
  return skeletonElements(shape.build(style))
}

// ---------------------------------------------------------------------------
// Linked records: cards that stand for a record of the operation. A click
// opens the record's picker; the card is placed once one is picked.

export const RECORD_TILES: { kind: RefKind; name: string; title: string; detail: string; keywords: string[] }[] = [
  { kind: "host", name: "Host", title: "DC01", detail: "10.0.5.10 · Windows Server", keywords: ["machine", "server", "ip"] },
  { kind: "credential", name: "Credential", title: "svc_backup", detail: "Password", keywords: ["password", "login", "secret"] },
  { kind: "hash", name: "Hash", title: "aad3b435b514…", detail: "Cracked", keywords: ["ntlm", "crack"] },
  { kind: "task", name: "Task", title: "Kerberoast", detail: "In process", keywords: ["todo", "kanban"] },
  { kind: "document", name: "Wiki page", title: "Recon notes", detail: "Wiki page", keywords: ["doc", "document", "note"] },
]

const recordPreviewCache = new Map<string, ExcalidrawElement[]>()
export function recordPreview(tile: (typeof RECORD_TILES)[number], style: ShapeStyle): ExcalidrawElement[] {
  const key = `${tile.kind}:${style}`
  let elements = recordPreviewCache.get(key)
  if (!elements) {
    const card = buildRefCard({ ref: { kind: tile.kind, id: "preview" }, title: tile.title, detail: tile.detail, style })
    elements = skeletonElements(card.elements)
    recordPreviewCache.set(key, elements)
  }
  return elements
}

export function matchesRecord(tile: (typeof RECORD_TILES)[number], query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  const haystack = [tile.name, "linked record", ...tile.keywords].join(" ").toLowerCase()
  return words.every((w) => haystack.includes(w))
}

/** Previews are converted once per shape and style and kept: the panel
 * re-renders on every keystroke in the search box. */
const previewCache = new Map<string, ExcalidrawElement[]>()
export function previewElements(shape: BuiltinShape, style: ShapeStyle): ExcalidrawElement[] {
  const key = `${shape.key}:${shape.styled ? style : "-"}`
  let elements = previewCache.get(key)
  if (!elements) {
    elements = buildShape(shape, style)
    previewCache.set(key, elements)
  }
  return elements
}


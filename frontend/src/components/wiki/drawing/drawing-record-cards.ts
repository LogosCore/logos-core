// Record cards on the canvas: the scene-reading half, pure so it is tested
// without a browser.
//
// A record card is a catalogue card whose tag carries `ref` (see "Record
// cards" in drawing-catalogue.ts). Every part carries the tag, and the parts
// share the card's own group, the first of their groupIds; an operator who
// groups the card with other shapes adds groups after it. That is how a
// frame finds its title and detail again, here and in the agents' reader.

import type { ExcalidrawElement, ExcalidrawTextElement } from "@excalidraw/excalidraw/element/types"
import { refKey, refOf, tagOf, type CardRef } from "./drawing-catalogue"

export interface RecordCard {
  ref: CardRef
  /** refKey(ref): two cards for one record share it. */
  key: string
  frame: ExcalidrawElement
  title: ExcalidrawTextElement | null
  detail: ExcalidrawTextElement | null
  /** Every part of the card, the frame included. */
  partIds: string[]
}

type Element = ExcalidrawElement & { points?: readonly (readonly [number, number])[] }

/** Every record card in a scene, in paint order. A frame without its group
 * (the operator ungrouped the card) is still a card; it just has no words
 * to refresh. */
export function recordCards(elements: readonly ExcalidrawElement[]): RecordCard[] {
  const live = elements.filter((el) => !el.isDeleted)
  const byGroup = new Map<string, ExcalidrawElement[]>()
  for (const el of live) {
    const group = el.groupIds[0]
    if (!group || !refOf(el)) continue
    const list = byGroup.get(group)
    if (list) list.push(el)
    else byGroup.set(group, [el])
  }
  const cards: RecordCard[] = []
  for (const el of live) {
    if (tagOf(el)?.part !== "frame") continue
    const ref = refOf(el)
    if (!ref) continue
    const parts = el.groupIds[0] ? (byGroup.get(el.groupIds[0]) ?? []) : []
    const text = (part: string) =>
      (parts.find((p) => p.type === "text" && tagOf(p)?.part === part && refOf(p)?.id === ref.id) as
        | ExcalidrawTextElement
        | undefined) ?? null
    cards.push({
      ref,
      key: refKey(ref),
      frame: el,
      title: text("title"),
      detail: text("detail"),
      partIds: parts.length > 0 ? parts.map((p) => p.id) : [el.id],
    })
  }
  return cards
}

/** The box an element covers, unrotated. A line's comes from its points. */
export function elementBox(el: ExcalidrawElement): { x: number; y: number; width: number; height: number } {
  const points = (el as Element).points
  if (points && points.length > 0) {
    const xs = points.map((p) => el.x + p[0])
    const ys = points.map((p) => el.y + p[1])
    const x = Math.min(...xs)
    const y = Math.min(...ys)
    return { x, y, width: Math.max(...xs) - x, height: Math.max(...ys) - y }
  }
  return { x: el.x, y: el.y, width: el.width, height: el.height }
}

/**
 * The record card under a scene point, topmost first: the record of the
 * highest part of a record card whose box holds the point. Anything else
 * above it is looked through; a zone or an arrow drawn over a card should
 * not make it unclickable.
 */
export function recordAt(elements: readonly ExcalidrawElement[], point: { x: number; y: number }): CardRef | null {
  const el = recordElementAt(elements, point)
  return el ? refOf(el) : null
}

/** The part of a record card under a scene point, as recordAt finds it. */
export function recordElementAt(
  elements: readonly ExcalidrawElement[],
  point: { x: number; y: number },
): ExcalidrawElement | null {
  for (let i = elements.length - 1; i >= 0; i--) {
    const el = elements[i]
    if (el.isDeleted || !refOf(el)) continue
    const box = elementBox(el)
    if (point.x >= box.x && point.x <= box.x + box.width && point.y >= box.y && point.y <= box.y + box.height) {
      return el
    }
  }
  return null
}

/**
 * `text`, cut with an ellipsis until `measure` says it fits `maxWidth`.
 * Refreshed words keep the card's size: a card wider than before would end
 * away from the arrows bound to its side.
 */
export function fitText(text: string, maxWidth: number, measure: (text: string) => number): string {
  if (maxWidth <= 0 || measure(text) <= maxWidth) return text
  const chars = [...text]
  let lo = 0
  let hi = chars.length
  // The longest prefix that fits with its ellipsis.
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2)
    if (measure(chars.slice(0, mid).join("").trimEnd() + "…") <= maxWidth) lo = mid
    else hi = mid - 1
  }
  return lo === 0 ? "…" : chars.slice(0, lo).join("").trimEnd() + "…"
}

/** "IN_PROCESS" → "In process". */
export function humanize(value: string): string {
  const words = value.toLowerCase().replace(/_/g, " ").trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** Record ids and record URLs in pasted content: a copied chip from a page
 * (its HTML carries data-*-id attributes), or links into the app. */
export function refsInPaste(html: string, text: string, origin: string): CardRef[] {
  const out: CardRef[] = []
  const seen = new Set<string>()
  const add = (ref: CardRef) => {
    const key = refKey(ref)
    if (seen.has(key)) return
    seen.add(key)
    out.push(ref)
  }
  const attrs: [RegExp, CardRef["kind"]][] = [
    [/data-host-id="([0-9A-Za-z_-]{1,64})"/g, "host"],
    [/data-credential-id="([0-9A-Za-z_-]{1,64})"/g, "credential"],
    [/data-hash-id="([0-9A-Za-z_-]{1,64})"/g, "hash"],
    [/data-document-id="([0-9A-Za-z_-]{1,64})"/g, "document"],
  ]
  for (const [re, kind] of attrs) {
    for (const m of html.matchAll(re)) add({ kind, id: m[1] })
  }
  if (out.length > 0) return out

  // Text becomes cards only when all of it is links to records in this app;
  // a sentence with a link in it is pasted as the sentence.
  const params: [string, CardRef["kind"]][] = [
    ["task", "task"],
    ["credential", "credential"],
    ["hash", "hash"],
    ["host", "host"],
  ]
  for (const word of text.split(/\s+/).filter(Boolean)) {
    let url: URL
    try {
      url = new URL(word, origin)
    } catch {
      return []
    }
    if (url.origin !== origin || !/^(https?:\/\/|\/)/.test(word)) return []
    let ref: CardRef | null = null
    const page = /^\/wiki\/([0-9A-Za-z_-]{1,64})\/?$/.exec(url.pathname)
    if (page) ref = { kind: "document", id: page[1] }
    for (const [param, kind] of params) {
      const id = url.searchParams.get(param)
      if (!ref && id && /^[0-9A-Za-z_-]{1,64}$/.test(id)) ref = { kind, id }
    }
    if (!ref) return []
    add(ref)
  }
  return out
}

interface Box {
  x: number
  y: number
  width: number
  height: number
}

const overlaps = (a: Box, b: Box, gap: number) =>
  a.x < b.x + b.width + gap && b.x < a.x + a.width + gap && a.y < b.y + b.height + gap && b.y < a.y + a.height + gap

/**
 * Where to centre a new box of `size` near `center` without covering any of
 * `taken`: the centre itself if it is free, else the first free place a row
 * at a time below it. A card added twice in a row lands under the first one
 * rather than on it.
 */
export function freeSpot(
  center: { x: number; y: number },
  size: { width: number; height: number },
  taken: readonly Box[],
  gap = 24,
): { x: number; y: number } {
  for (let row = 0; row < 40; row++) {
    const y = center.y + row * (size.height + gap)
    const box = { x: center.x - size.width / 2, y: y - size.height / 2, ...size }
    if (!taken.some((t) => overlaps(box, t, gap))) return { x: center.x, y }
  }
  return center
}

/** At most `max` characters, cut with an ellipsis: a card is sized to its
 * words, and a task's name can be a sentence. */
export function clip(text: string, max: number): string {
  const chars = [...text.trim()]
  return chars.length <= max ? chars.join("") : chars.slice(0, max - 1).join("").trimEnd() + "…"
}

// ---------------------------------------------------------------------------
// Resizing a card
//
// A card is a group, and Excalidraw resizes a group by scaling all of it:
// the text grows with the box and never gets more room. For a card that is
// not what a resize means. The canvas keeps the card's geometry from before
// the resize, and once the resize ends turns the scaling into width: every
// part back to its own size, the frame as wide as the resize made it, the
// badges on its new corner (use-record-cards.ts refits the words).

/** The parts of an element a resize changes. */
export interface PartGeometry {
  x: number
  y: number
  width: number
  height: number
  fontSize?: number
  points?: readonly (readonly [number, number])[]
}

export function geometryOf(el: ExcalidrawElement): PartGeometry {
  const g: PartGeometry = { x: el.x, y: el.y, width: el.width, height: el.height }
  if ("fontSize" in el) g.fontSize = el.fontSize
  const points = (el as Element).points
  if (points) g.points = points.map((p) => [p[0], p[1]] as const)
  return g
}

/** Where a resized card's frame goes: its left edge and width. */
export interface WidthTarget {
  x: number
  width: number
}

/**
 * The width a resize of the whole card (Excalidraw's own, which scales it)
 * stands for, at least `minWidth`. The edge the resize was anchored on
 * stays where it was. Null when the resize changed nothing.
 */
export function resizeTarget(frameBefore: Box, frameNow: Box, minWidth: number, maxWidth = 1600): WidthTarget | null {
  const width = Math.min(maxWidth, Math.max(minWidth, Math.round(frameNow.width)))
  const same = (a: number, b: number) => Math.abs(a - b) < 0.5
  if (same(width, frameBefore.width) && same(frameNow.height, frameBefore.height) && same(frameNow.x, frameBefore.x)) {
    return null
  }
  // Dragged from the right (left edge fixed), from the left (right edge
  // fixed), or about the centre (Alt).
  if (same(frameNow.x, frameBefore.x)) return { x: frameBefore.x, width }
  if (same(frameNow.x + frameNow.width, frameBefore.x + frameBefore.width)) {
    return { x: frameBefore.x + frameBefore.width - width, width }
  }
  return { x: frameNow.x + frameNow.width / 2 - width / 2, width }
}

/**
 * The card at a new width: every part at its own size, moved with the left
 * edge, the frame as wide as `target` says, the badges on its new corner.
 * The card keeps its height and its place up and down.
 */
export function widenCard(
  before: ReadonlyMap<string, PartGeometry>,
  frameId: string,
  frameBefore: Box,
  target: WidthTarget,
  badgeIds: ReadonlySet<string>,
): { frame: Box; parts: Map<string, PartGeometry> } {
  const frame = { x: target.x, y: frameBefore.y, width: target.width, height: frameBefore.height }
  const dx = frame.x - frameBefore.x
  const grow = frame.width - frameBefore.width
  const parts = new Map<string, PartGeometry>()
  for (const [id, g] of before) {
    if (id === frameId) parts.set(id, { ...g, ...frame })
    else parts.set(id, { ...g, x: g.x + dx + (badgeIds.has(id) ? grow : 0) })
  }
  return { frame, parts }
}

/** A point on box `from` to the same place on box `to`: for the ends of the
 * arrows bound to a card that changed size. */
export function boxMap(from: Box, to: Box): (p: { x: number; y: number }) => { x: number; y: number } {
  return (p) => ({
    x: to.x + ((p.x - from.x) / (from.width || 1)) * to.width,
    y: to.y + ((p.y - from.y) / (from.height || 1)) * to.height,
  })
}

/**
 * An arrow with its bound end(s) moved by `map`: `ends` says which. An
 * elbow arrow keeps its right angles, its segment into the moved end
 * following it along the other axis. Returns the arrow's new origin and
 * points, and its box.
 */
export function moveArrowEnds(
  arrow: { x: number; y: number; points: readonly (readonly [number, number])[]; elbowed?: boolean },
  ends: { start: boolean; end: boolean },
  map: (p: { x: number; y: number }) => { x: number; y: number },
): { x: number; y: number; width: number; height: number; points: [number, number][] } {
  const abs = arrow.points.map((p) => ({ x: arrow.x + p[0], y: arrow.y + p[1] }))
  const move = (i: number, neighbour: number) => {
    const old = abs[i]
    const next = map(old)
    abs[i] = next
    if (!arrow.elbowed || neighbour < 0 || neighbour >= abs.length) return
    const n = abs[neighbour]
    // The segment into the end was horizontal or vertical; keep it so.
    if (Math.abs(n.y - old.y) < 0.5) abs[neighbour] = { x: n.x, y: next.y }
    else if (Math.abs(n.x - old.x) < 0.5) abs[neighbour] = { x: next.x, y: n.y }
  }
  if (abs.length > 0 && ends.start) move(0, 1)
  if (abs.length > 1 && ends.end) move(abs.length - 1, abs.length - 2)
  const origin = abs[0] ?? { x: arrow.x, y: arrow.y }
  const points = abs.map((p) => [p.x - origin.x, p.y - origin.y] as [number, number])
  const xs = points.map((p) => p[0])
  const ys = points.map((p) => p[1])
  return {
    x: origin.x,
    y: origin.y,
    width: Math.max(...xs) - Math.min(...xs),
    height: Math.max(...ys) - Math.min(...ys),
    points,
  }
}

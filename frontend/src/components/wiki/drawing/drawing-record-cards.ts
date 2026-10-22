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
  for (let i = elements.length - 1; i >= 0; i--) {
    const el = elements[i]
    if (el.isDeleted) continue
    const ref = refOf(el)
    if (!ref) continue
    const box = elementBox(el)
    if (point.x >= box.x && point.x <= box.x + box.width && point.y >= box.y && point.y <= box.y + box.height) {
      return ref
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

// Record cards on a live canvas: clicking one opens its record, its words
// are refreshed from the record when the drawing opens, a card whose record
// is gone says so, and new ones come from the Shapes panel's pickers or a
// paste of a chip or a record's link.
//
// How a card opens, by mode:
//   - viewing (a viewer, or view mode): a click anywhere on the card;
//   - editing: a click selects, as for any shape, and the card then shows an
//     Open button (drawing-record-overlay.tsx). Ctrl/⌘-click opens directly.
//     Cards drawn before record cards dropped their element link still carry
//     one; its handle opens the record too, and a refresh takes it off.
//
// Refreshing writes to the shared scene, so only editors do it, once per
// card per visit. The card keeps its size: words that no longer fit are cut
// with an ellipsis, since a wider card would leave its arrows behind.

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import {
  CaptureUpdateAction,
  convertToExcalidrawElements,
  hashElementsVersion,
  newElementWith,
  sceneCoordsToViewportCoords,
  viewportCoordsToSceneCoords,
} from "@excalidraw/excalidraw"
import type { AppState, ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types"
import type {
  ExcalidrawElement,
  ExcalidrawTextElement,
  NonDeletedExcalidrawElement,
} from "@excalidraw/excalidraw/element/types"

import { buildRefCard, refKey, refOf, tagOf, type CardRef, type RefKind, type ShapeStyle } from "./drawing-catalogue"
import {
  clip,
  elementBox,
  fitText,
  freeSpot,
  recordAt,
  recordCards,
  refsInPaste,
  type RecordCard,
} from "./drawing-record-cards"
import { REF_LABELS, lookupRecord, pickRecord, type RecordLookup } from "./drawing-records"
import { insertElements, skeletonElements } from "./drawing-place"
import { boundsOf, centeredAt, viewportCenter } from "./drawing-insert"

/** Something drawn over a card, in the canvas's own pixels. */
export interface RecordOverlayItem {
  /** The card's frame id. */
  id: string
  ref: CardRef
  x: number
  y: number
  width: number
  height: number
  /** "open": the selected card's Open button; otherwise the card's record is
   * gone or out of reach. */
  kind: "open" | "missing" | "forbidden"
}

interface UseRecordCardsArgs {
  api: ExcalidrawImperativeAPI | null
  isEditor: boolean
  operationId: string
  documentId: string
  /** The element the canvas fills; pointer handling listens on it. */
  containerRef: React.RefObject<HTMLElement | null>
}

// A press that moves further than this, or lasts longer, is a drag or a
// long-press, not a click.
const CLICK_SLOP_PX = 5
const CLICK_MAX_MS = 700
// Excalidraw's own link handling can follow ours for the same click; the
// second open within this window is the same click.
const DOUBLE_OPEN_MS = 600
// Room a card leaves right of its words (CARD_PAD in the catalogue).
const CARD_PAD = 14
// The longest words a new card is given; a refresh keeps the card's width.
const TITLE_MAX = 36
const DETAIL_MAX = 48

export function useRecordCards({ api, isEditor, operationId, documentId, containerRef }: UseRecordCardsArgs) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [lookups, setLookups] = useState<ReadonlyMap<string, RecordLookup>>(() => new Map())
  const requestedRef = useRef(new Set<string>())
  const [cards, setCards] = useState<RecordCard[]>([])
  const hashRef = useRef<number | null>(null)
  const [overlay, setOverlay] = useState<RecordOverlayItem[]>([])
  const overlayKeyRef = useRef("")
  const lastOpenRef = useRef({ key: "", at: 0 })

  // --- Records ---------------------------------------------------------------

  const request = useCallback(
    (refs: readonly CardRef[], force = false) => {
      for (const ref of refs) {
        const key = refKey(ref)
        if (!force && requestedRef.current.has(key)) continue
        requestedRef.current.add(key)
        void lookupRecord(queryClient, ref).then((lookup) => {
          setLookups((prev) => new Map(prev).set(key, lookup))
        })
      }
    },
    [queryClient],
  )

  const open = useCallback(
    async (ref: CardRef, newTab = false) => {
      const key = refKey(ref)
      const now = Date.now()
      if (lastOpenRef.current.key === key && now - lastOpenRef.current.at < DOUBLE_OPEN_MS) return
      lastOpenRef.current = { key, at: now }
      const lookup = await lookupRecord(queryClient, ref)
      setLookups((prev) => new Map(prev).set(key, lookup))
      const label = REF_LABELS[ref.kind]
      switch (lookup.status) {
        case "ok":
          lookup.open({ navigate, newTab })
          return
        case "missing":
          toast.error(`This ${label} no longer exists`, { description: "It may have been deleted." })
          return
        case "forbidden":
          toast.error(`You don't have access to this ${label}`)
          return
        case "error":
          toast.error(`Could not load this ${label}`, { description: "Check your connection and try again." })
      }
    },
    [queryClient, navigate],
  )

  // --- Scene ------------------------------------------------------------------

  /** Called from the canvas's onChange: finds the cards when the drawing
   * changed, and keeps the overlay over them as the view moves. */
  const onChange = useCallback(
    (elements: readonly ExcalidrawElement[], appState: AppState) => {
      const hash = hashElementsVersion(elements)
      let current = cards
      if (hash !== hashRef.current) {
        hashRef.current = hash
        current = recordCards(elements)
        setCards(current)
        request(current.map((c) => c.ref))
      }
      const items = overlayItems(current, lookups, appState)
      const key = JSON.stringify(items)
      if (key !== overlayKeyRef.current) {
        overlayKeyRef.current = key
        setOverlay(items)
      }
    },
    [cards, lookups, request],
  )

  // A lookup that lands moves the overlay without a canvas change.
  useEffect(() => {
    if (!api) return
    const items = overlayItems(cards, lookups, api.getAppState())
    const key = JSON.stringify(items)
    if (key !== overlayKeyRef.current) {
      overlayKeyRef.current = key
      setOverlay(items)
    }
  }, [api, cards, lookups])

  // --- Refresh ------------------------------------------------------------------

  const refreshedRef = useRef(new Set<string>())
  useEffect(() => {
    if (!api || !isEditor) return
    const updates = new Map<string, ExcalidrawElement>()
    const added: ExcalidrawElement[] = []
    for (const card of cards) {
      if (refreshedRef.current.has(card.frame.id)) continue
      const lookup = lookups.get(card.key)
      if (!lookup) continue
      refreshedRef.current.add(card.frame.id)
      // An early card's element link: its handle sat on the badges' corner.
      if (card.frame.link) updates.set(card.frame.id, newElementWith(card.frame, { link: null }))
      if (lookup.status !== "ok") continue
      const words = [
        [card.title, lookup.title, TITLE_MAX],
        [card.detail, lookup.detail, DETAIL_MAX],
      ] as const
      let right = card.frame.x + card.frame.width - CARD_PAD
      // A card no arrow is bound to grows to fit its record's words (an
      // agent may have drawn it with a placeholder). One with arrows keeps
      // its size, as its arrows end at its edge.
      const bound = (card.frame.boundElements ?? []).some((b) => b.type === "arrow")
      if (!bound) {
        const need = Math.max(
          ...words.map(([el, text, max]) =>
            el ? el.x - card.frame.x + measureText(el, clip(text, max)).width + CARD_PAD : 0,
          ),
        )
        const delta = Math.ceil(need - card.frame.width)
        if (delta > 0) {
          const frame = (updates.get(card.frame.id) as typeof card.frame | undefined) ?? card.frame
          updates.set(card.frame.id, newElementWith(frame, { width: card.frame.width + delta }))
          // Badges sit on the right corner, and move with it.
          for (const part of api.getSceneElements()) {
            if (card.partIds.includes(part.id) && tagOf(part)?.part.startsWith("badge:")) {
              updates.set(part.id, newElementWith(part, { x: part.x + delta }))
            }
          }
          right += delta
        }
      }
      for (const [el, text] of words) {
        if (!el) continue
        const next = retext(el, text, right - el.x)
        if (next) updates.set(el.id, next)
      }
      // A card drawn without a detail line (an agent's, with only a title)
      // gets one when its record has something to say: the title moves up
      // to where a card with a detail has it.
      if (card.title && !card.detail && lookup.detail) {
        const title = (updates.get(card.title.id) as ExcalidrawTextElement | undefined) ?? card.title
        const top = card.frame.y + CARD_PAD
        updates.set(title.id, newElementWith(title, { y: top + 2 }))
        added.push(detailLine(title, top + 30, fitText(lookup.detail, right - title.x, (t) => measureText(title, t, 14).width)))
      }
    }
    if (updates.size === 0 && added.length === 0) return
    api.updateScene({
      elements: [...api.getSceneElementsIncludingDeleted().map((el) => updates.get(el.id) ?? el), ...added],
      // Not an edit of the operator's: undo should not bring the old name back.
      captureUpdate: CaptureUpdateAction.NEVER,
    })
  }, [api, isEditor, cards, lookups])

  // --- Placing cards ------------------------------------------------------------

  const style = useCallback((): ShapeStyle => {
    // New cards follow the panel's style switch.
    try {
      const prefs = JSON.parse(localStorage.getItem("wiki_drawing_shapes_panel") ?? "null") as { style?: string } | null
      return prefs?.style === "clean" ? "clean" : "sketch"
    } catch {
      return "sketch"
    }
  }, [])

  const place = useCallback(
    async (refs: readonly CardRef[]) => {
      if (!api || refs.length === 0) return
      const lookupsNow = await Promise.all(refs.map((ref) => lookupRecord(queryClient, ref)))
      const built: ExcalidrawElement[][] = []
      lookupsNow.forEach((lookup, i) => {
        const ref = refs[i]
        if (lookup.status !== "ok") {
          toast.error(`Could not add this ${REF_LABELS[ref.kind]}`, {
            description:
              lookup.status === "forbidden"
                ? "You don't have access to it."
                : lookup.status === "missing"
                  ? "It no longer exists."
                  : "Check your connection and try again.",
          })
          return
        }
        setLookups((prev) => new Map(prev).set(refKey(ref), lookup))
        const card = buildRefCard({
          ref,
          title: clip(lookup.title, TITLE_MAX),
          detail: clip(lookup.detail, DETAIL_MAX),
          style: style(),
          icon: lookup.icon,
        })
        built.push(skeletonElements(card.elements))
      })
      if (built.length === 0) return
      // Several cards in a row, a card's width apart.
      let x = 0
      const row = built.flatMap((card) => {
        const moved = centeredAt(card, { x: 0, y: 0 }).map((el) => ({ ...el, x: el.x + x }) as ExcalidrawElement)
        const width = Math.max(...card.map((el) => el.x + el.width)) - Math.min(...card.map((el) => el.x))
        x += width + 40
        return moved
      })
      // Below the cards already there, not on top of them.
      const b = boundsOf(row)
      const taken = recordCards(api.getSceneElements()).map((c) => elementBox(c.frame))
      const center = freeSpot(viewportCenter(api.getAppState()), { width: b.maxX - b.minX, height: b.maxY - b.minY }, taken)
      insertElements(api, row, center)
    },
    [api, queryClient, style],
  )

  /** The Shapes panel's record tiles: pick a record, then place its card. */
  const addRecord = useCallback(
    (kind: RefKind) => pickRecord(kind, operationId, (ref) => void place([ref]), [documentId]),
    [operationId, documentId, place],
  )

  /** Excalidraw's onPaste: a copied chip, or links to records, become cards. */
  const onPaste = useCallback(
    (data: { elements?: readonly unknown[] | null; files?: unknown }, event: ClipboardEvent | null) => {
      if (!isEditor || !event?.clipboardData || (data.elements && data.elements.length > 0)) return true
      const refs = refsInPaste(
        event.clipboardData.getData("text/html"),
        event.clipboardData.getData("text/plain"),
        window.location.origin,
      )
      if (refs.length === 0) return true
      void place(refs)
      return false
    },
    [isEditor, place],
  )

  /** Excalidraw's onLinkOpen: a record card's link opens the record here. */
  const onLinkOpen = useCallback(
    (element: NonDeletedExcalidrawElement, event: CustomEvent<{ nativeEvent: MouseEvent | React.PointerEvent }>) => {
      const ref = refOf(element)
      if (!ref) return
      event.preventDefault()
      const native = event.detail.nativeEvent as MouseEvent
      void open(ref, native.ctrlKey || native.metaKey)
    },
    [open],
  )

  // --- Clicks -----------------------------------------------------------------

  useEffect(() => {
    const el = containerRef.current
    if (!el || !api) return
    let press: { x: number; y: number; at: number; direct: boolean } | null = null
    const hit = (e: PointerEvent) => {
      const state = api.getAppState()
      const point = viewportCoordsToSceneCoords({ clientX: e.clientX, clientY: e.clientY }, state)
      return recordAt(api.getSceneElements(), point)
    }
    const onCanvas = (e: Event) => (e.target as HTMLElement | null)?.tagName === "CANVAS"

    const down = (e: PointerEvent) => {
      press = null
      if (e.button !== 0 || !onCanvas(e)) return
      const viewing = api.getAppState().viewModeEnabled
      const modifier = e.ctrlKey || e.metaKey
      if (!viewing && !modifier) return
      if (!hit(e)) return
      press = { x: e.clientX, y: e.clientY, at: Date.now(), direct: modifier }
      // Ctrl/⌘ on a card while editing is ours: without this Excalidraw
      // would also select inside the card's group.
      if (!viewing) {
        e.stopPropagation()
        e.preventDefault()
      }
    }
    const up = (e: PointerEvent) => {
      const p = press
      press = null
      if (!p || e.button !== 0) return
      if (Math.hypot(e.clientX - p.x, e.clientY - p.y) > CLICK_SLOP_PX || Date.now() - p.at > CLICK_MAX_MS) return
      const ref = hit(e)
      if (ref) void open(ref, p.direct && api.getAppState().viewModeEnabled)
    }
    // A hand over a card says it can be clicked. Excalidraw sets the
    // canvas's cursor on every move, from React's handler, which runs after
    // this listener; so the hand goes on a frame later.
    let frame = 0
    const move = (e: PointerEvent) => {
      if (!onCanvas(e) || e.buttons !== 0) return
      const viewing = api.getAppState().viewModeEnabled
      if (!viewing && !(e.ctrlKey || e.metaKey)) return
      if (!hit(e)) return
      const canvas = e.target as HTMLElement
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        canvas.style.cursor = "pointer"
      })
    }
    el.addEventListener("pointerdown", down, true)
    el.addEventListener("pointerup", up, true)
    el.addEventListener("pointermove", move)
    return () => {
      el.removeEventListener("pointerdown", down, true)
      el.removeEventListener("pointerup", up, true)
      el.removeEventListener("pointermove", move)
      cancelAnimationFrame(frame)
    }
  }, [api, containerRef, open])

  const openCard = useCallback((ref: CardRef) => void open(ref), [open])

  return useMemo(
    () => ({ onChange, onLinkOpen, onPaste, overlay, addRecord, openCard }),
    [onChange, onLinkOpen, onPaste, overlay, addRecord, openCard],
  )
}

/** What the overlay draws: an Open button under the one selected card, and a
 * marker on each card whose record is gone. */
function overlayItems(
  cards: readonly RecordCard[],
  lookups: ReadonlyMap<string, RecordLookup>,
  appState: AppState,
): RecordOverlayItem[] {
  const items: RecordOverlayItem[] = []
  const box = (card: RecordCard) => {
    const { frame } = card
    const a = sceneCoordsToViewportCoords({ sceneX: frame.x, sceneY: frame.y }, appState)
    const b = sceneCoordsToViewportCoords({ sceneX: frame.x + frame.width, sceneY: frame.y + frame.height }, appState)
    return {
      x: a.x - appState.offsetLeft,
      y: a.y - appState.offsetTop,
      width: b.x - a.x,
      height: b.y - a.y,
    }
  }
  // The Open button is for one card selected on its own: with more selected
  // it would be ambiguous, and a box selection over a map would sprout one
  // per card.
  const selected = Object.keys(appState.selectedElementIds)
  if (!appState.viewModeEnabled && selected.length > 0 && !appState.editingTextElement && !appState.newElement) {
    const card = cards.find((c) => appState.selectedElementIds[c.frame.id])
    if (card && selected.every((id) => card.partIds.includes(id))) {
      items.push({ id: card.frame.id, ref: card.ref, kind: "open", ...box(card) })
    }
  }
  for (const card of cards) {
    const lookup = lookups.get(card.key)
    if (lookup?.status === "missing" || lookup?.status === "forbidden") {
      items.push({ id: card.frame.id, ref: card.ref, kind: lookup.status, ...box(card) })
    }
  }
  return items
}

/** `el` with new words fitted to `maxWidth`, or null when it already shows
 * them. */
function retext(el: ExcalidrawTextElement, words: string, maxWidth: number): ExcalidrawElement | null {
  const measure = (text: string) => measureText(el, text)
  // Already the record's words, or a cut of them that fits (the one a card
  // is placed with): leave it.
  const shown = el.originalText || el.text
  const cut = shown.length > 1 && shown.endsWith("…") && words.startsWith(shown.slice(0, -1).trimEnd())
  if (shown === words || (cut && measure(shown).width <= maxWidth)) return null
  const text = fitText(words, maxWidth, (t) => measure(t).width)
  if (text === el.text && text === el.originalText) return null
  const size = measure(text)
  return newElementWith(el, { text, originalText: text, width: size.width, height: size.height })
}

/** The size of `text` in the font of `el` (at `fontSize`, or its own),
 * measured by Excalidraw itself. */
function measureText(el: ExcalidrawTextElement, text: string, fontSize = el.fontSize): { width: number; height: number } {
  const [probe] = convertToExcalidrawElements([
    { type: "text", x: 0, y: 0, text: text || " ", fontSize, fontFamily: el.fontFamily },
  ])
  return { width: probe.width, height: probe.height }
}

// The catalogue's detail line: 14px, muted (drawing-catalogue.ts, cardShapes).
const DETAIL_SIZE = 14
const DETAIL_COLOR = "#868e96"

/** A detail line for the card `title` belongs to, tagged as its part. */
function detailLine(title: ExcalidrawTextElement, y: number, text: string): ExcalidrawElement {
  const tag = tagOf(title)
  const [line] = convertToExcalidrawElements([
    {
      type: "text",
      x: title.x,
      y,
      text,
      fontSize: DETAIL_SIZE,
      fontFamily: title.fontFamily,
      strokeColor: DETAIL_COLOR,
      groupIds: [...title.groupIds],
      customData: tag ? { logos: { ...tag, part: "detail" } } : undefined,
    } as never,
  ])
  return line
}

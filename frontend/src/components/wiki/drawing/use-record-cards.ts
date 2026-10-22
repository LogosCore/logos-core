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
// card per visit. A card with arrows keeps its size, its words cut with an
// ellipsis, since a wider card would leave its arrows behind; resizing it
// makes it wider (see "Resizing a card" in drawing-record-cards.ts), and
// hovering shows the words in full.

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
  geometryOf,
  moveArrowEnds,
  recordAt,
  recordCards,
  recordElementAt,
  refsInPaste,
  resizeTarget,
  boxMap,
  widenCard,
  type PartGeometry,
  type RecordCard,
  type WidthTarget,
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
  /** "open": the selected card's Open button; "hover": the full words of the
   * card under the pointer; otherwise the card's record is gone or out of
   * reach. */
  kind: "open" | "hover" | "missing" | "forbidden"
  /** For "hover": the record's words. */
  title?: string
  detail?: string
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
const TITLE_MAX = 60
const DETAIL_MAX = 72
// The narrowest a resize leaves a card: the icon tile and a few letters.
const MIN_CARD_WIDTH = 160
// How long the pointer rests on a card before its full words show.
const HOVER_DELAY_MS = 350

/** An arrow bound to a card, as a resize found it. */
interface BoundArrow {
  x: number
  y: number
  points: readonly (readonly [number, number])[]
  elbowed?: boolean
  start: boolean
  end: boolean
}

/** A card's geometry from before a resize: see onChange. */
interface CardSnapshot {
  frameId: string
  frame: { x: number; y: number; width: number; height: number }
  parts: Map<string, PartGeometry>
  badges: Set<string>
  arrows: Map<string, BoundArrow>
}

type ArrowElement = ExcalidrawElement & {
  points: readonly (readonly [number, number])[]
  elbowed?: boolean
  startBinding?: { elementId: string } | null
  endBinding?: { elementId: string } | null
}

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
  const [hovered, setHovered] = useState<string | null>(null)
  const cardsRef = useRef<RecordCard[]>([])
  const lookupsRef = useRef(lookups)
  useEffect(() => {
    lookupsRef.current = lookups
  }, [lookups])

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
  /** The card of `snapshot` at the width `target` says: its parts, its
   * words refitted, and the arrows bound to it ending on its new edge. The
   * arrows are taken as they are on `arrowsOn` and moved from it. */
  const widenUpdates = useCallback(
    (snapshot: CardSnapshot, target: WidthTarget, arrowsOn: Box, arrows: ReadonlyMap<string, BoundArrow>) => {
      const updates = new Map<string, ExcalidrawElement>()
      if (!api) return updates
      const byId = new Map(api.getSceneElementsIncludingDeleted().map((el) => [el.id, el]))
      const widened = widenCard(snapshot.parts, snapshot.frameId, snapshot.frame, target, snapshot.badges)
      for (const [id, g] of widened.parts) {
        const el = byId.get(id)
        if (el) updates.set(id, newElementWith(el, g as never))
      }
      // The words, refitted to the new width from the record's own.
      const right = widened.frame.x + widened.frame.width - CARD_PAD
      for (const el of [...updates.values()]) {
        const part = tagOf(el)?.part
        if (el.type !== "text" || (part !== "title" && part !== "detail")) continue
        const ref = refOf(el)
        const lookup = ref ? lookupsRef.current.get(refKey(ref)) : undefined
        const text = el as ExcalidrawTextElement
        const words = lookup?.status === "ok" ? lookup[part] : text.originalText
        const next = retext(text, words, right - el.x)
        if (next) updates.set(el.id, next)
      }
      const map = boxMap(arrowsOn, widened.frame)
      for (const [id, arrow] of arrows) {
        const el = byId.get(id)
        if (!el || el.isDeleted) continue
        updates.set(id, newElementWith(el, moveArrowEnds(arrow, arrow, map) as never))
      }
      return updates
    },
    [api],
  )

  const apply = useCallback(
    (updates: ReadonlyMap<string, ExcalidrawElement>, captureUpdate: (typeof CaptureUpdateAction)[keyof typeof CaptureUpdateAction]) => {
      if (!api || updates.size === 0) return
      api.updateScene({
        elements: api.getSceneElementsIncludingDeleted().map((el) => updates.get(el.id) ?? el),
        captureUpdate,
      })
    },
    [api],
  )

  /** Excalidraw's own resize of a card (a touch, or a handle the canvas did
   * not take over) scaled it; make it a change of width after the fact. */
  const finishResize = useCallback(
    (snapshot: CardSnapshot) => {
      if (!api) return
      const all = api.getSceneElementsIncludingDeleted()
      const frameNow = all.find((el) => el.id === snapshot.frameId)
      if (!frameNow || frameNow.isDeleted) return
      const now = elementBox(frameNow)
      const target = resizeTarget(snapshot.frame, now, MIN_CARD_WIDTH)
      if (!target) return
      // Excalidraw moved the arrows with the scaled card: move them on from
      // where it left them.
      const arrows = new Map<string, BoundArrow>()
      for (const [id] of snapshot.arrows) {
        const a = all.find((el) => el.id === id) as ArrowElement | undefined
        if (a) arrows.set(id, { ...arrowOf(a, snapshot.frameId) })
      }
      // Not a step of its own: it finishes the resize the operator made.
      apply(widenUpdates(snapshot, target, now, arrows), CaptureUpdateAction.NEVER)
    },
    [api, apply, widenUpdates],
  )

  const snapshotRef = useRef<CardSnapshot | null>(null)
  const snapshotKeyRef = useRef("")
  const resizingRef = useRef(false)

  const onChange = useCallback(
    (elements: readonly ExcalidrawElement[], appState: AppState) => {
      const hash = hashElementsVersion(elements)
      let current = cards
      const changed = hash !== hashRef.current
      if (changed) {
        hashRef.current = hash
        current = recordCards(elements)
        cardsRef.current = current
        setCards(current)
        request(current.map((c) => c.ref))
      }

      // A resize of one card on its own ends as a wider card, not a bigger
      // one. Its geometry is kept while it is selected and still, and
      // compared with what the resize left once the resize ends.
      if (isEditor) {
        if (resizingRef.current && !appState.isResizing && snapshotRef.current) {
          const snapshot = snapshotRef.current
          requestAnimationFrame(() => finishResize(snapshot))
        }
        resizingRef.current = appState.isResizing
        if (!appState.isResizing) {
          const selection = Object.keys(appState.selectedElementIds)
          const key = `${hash}:${selection.join(",")}`
          if (key !== snapshotKeyRef.current) {
            snapshotKeyRef.current = key
            snapshotRef.current = snapshotOf(soleSelectedCard(current, selection), elements)
          }
        }
      }

      const items = overlayItems(current, lookups, appState, appState.isResizing ? null : hovered)
      const key = JSON.stringify(items)
      if (key !== overlayKeyRef.current) {
        overlayKeyRef.current = key
        setOverlay(items)
      }
    },
    [cards, lookups, request, isEditor, hovered, finishResize],
  )

  // A lookup that lands, or the pointer resting on a card, changes the
  // overlay without a canvas change.
  useEffect(() => {
    if (!api) return
    const items = overlayItems(cards, lookups, api.getAppState(), hovered)
    const key = JSON.stringify(items)
    if (key !== overlayKeyRef.current) {
      overlayKeyRef.current = key
      setOverlay(items)
    }
  }, [api, cards, lookups, hovered])


  // --- Refresh ------------------------------------------------------------------

  const refreshedRef = useRef(new Set<string>())
  useEffect(() => {
    if (!api || !isEditor) return
    const updates = new Map<string, ExcalidrawElement>()
    const added: ExcalidrawElement[] = []
    for (const card of cards) {
      if (refreshedRef.current.has(card.frame.id)) {
        // Words that run past the card's edge (an undone resize leaves the
        // wide card's words in the narrow card) are refitted.
        const lookup = lookups.get(card.key)
        const right = card.frame.x + card.frame.width - CARD_PAD
        for (const [el, part] of [
          [card.title, "title"],
          [card.detail, "detail"],
        ] as const) {
          if (!el || el.x + el.width <= right + 1) continue
          const words = lookup?.status === "ok" ? lookup[part] : el.originalText
          const next = retext(el, words, right - el.x)
          if (next) updates.set(el.id, next)
        }
        continue
      }
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

    // A corner handle of a record card selected on its own: the canvas takes
    // the drag over, so the card widens rather than scales. Excalidraw draws
    // the handles a few pixels out from the corners of the group's box.
    const HANDLE_REACH_PX = 12
    const grabbedCorner = (e: PointerEvent): { snapshot: CardSnapshot; side: "left" | "right" } | null => {
      const state = api.getAppState()
      if (!isEditor || state.viewModeEnabled) return null
      const card = soleSelectedCard(cardsRef.current, Object.keys(state.selectedElementIds))
      if (!card) return null
      const parts = api.getSceneElements().filter((p) => card.partIds.includes(p.id))
      if (parts.length === 0) return null
      const boxes = parts.map(elementBox)
      const a = sceneCoordsToViewportCoords(
        { sceneX: Math.min(...boxes.map((b) => b.x)), sceneY: Math.min(...boxes.map((b) => b.y)) },
        state,
      )
      const b = sceneCoordsToViewportCoords(
        {
          sceneX: Math.max(...boxes.map((b) => b.x + b.width)),
          sceneY: Math.max(...boxes.map((b) => b.y + b.height)),
        },
        state,
      )
      for (const [cx, cy, side] of [
        [a.x, a.y, "left"],
        [a.x, b.y, "left"],
        [b.x, a.y, "right"],
        [b.x, b.y, "right"],
      ] as const) {
        if (Math.hypot(e.clientX - cx, e.clientY - cy) <= HANDLE_REACH_PX) {
          const snapshot = snapshotOf(card, api.getSceneElements())
          return snapshot ? { snapshot, side } : null
        }
      }
      return null
    }

    const widenDrag = (e: PointerEvent, grab: { snapshot: CardSnapshot; side: "left" | "right" }) => {
      const { snapshot, side } = grab
      const startX = e.clientX
      const zoom = api.getAppState().zoom.value
      let last: Map<string, ExcalidrawElement> | null = null
      const target = (clientX: number): WidthTarget => {
        const dx = (clientX - startX) / zoom
        const { x, width: w } = snapshot.frame
        const width = Math.min(1600, Math.max(MIN_CARD_WIDTH, Math.round(side === "right" ? w + dx : w - dx)))
        return { x: side === "right" ? x : x + w - width, width }
      }
      const onMove = (ev: PointerEvent) => {
        ev.stopPropagation()
        last = widenUpdates(snapshot, target(ev.clientX), snapshot.frame, snapshot.arrows)
        // Steps of one change: the whole drag is one undo.
        apply(last, CaptureUpdateAction.EVENTUALLY)
      }
      const onUp = (ev: PointerEvent) => {
        ev.stopPropagation()
        window.removeEventListener("pointermove", onMove, true)
        window.removeEventListener("pointerup", onUp, true)
        if (last) apply(widenUpdates(snapshot, target(ev.clientX), snapshot.frame, snapshot.arrows), CaptureUpdateAction.IMMEDIATELY)
      }
      window.addEventListener("pointermove", onMove, true)
      window.addEventListener("pointerup", onUp, true)
    }

    const down = (e: PointerEvent) => {
      press = null
      if (e.button !== 0 || !onCanvas(e)) return
      const grab = grabbedCorner(e)
      if (grab) {
        e.stopPropagation()
        e.preventDefault()
        hover(null)
        widenDrag(e, grab)
        return
      }
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
    // The card the pointer rests on, for its full words.
    let hoverTimer: ReturnType<typeof setTimeout> | undefined
    let hoverId: string | null = null
    const hover = (id: string | null) => {
      if (id === hoverId) return
      hoverId = id
      clearTimeout(hoverTimer)
      if (id === null) setHovered(null)
      else hoverTimer = setTimeout(() => setHovered(id), HOVER_DELAY_MS)
    }
    const leave = () => hover(null)
    const move = (e: PointerEvent) => {
      if (!onCanvas(e) || e.buttons !== 0) {
        hover(null)
        return
      }
      const state = api.getAppState()
      const under = recordElementAt(
        api.getSceneElements(),
        viewportCoordsToSceneCoords({ clientX: e.clientX, clientY: e.clientY }, state),
      )
      const card = under ? cardsRef.current.find((c) => c.partIds.includes(under.id)) : undefined
      hover(card ? card.frame.id : null)
      const viewing = state.viewModeEnabled
      if (!viewing && !(e.ctrlKey || e.metaKey)) return
      if (!under) return
      const canvas = e.target as HTMLElement
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        canvas.style.cursor = "pointer"
      })
    }
    el.addEventListener("pointerdown", down, true)
    el.addEventListener("pointerup", up, true)
    el.addEventListener("pointermove", move)
    el.addEventListener("pointerleave", leave)
    el.addEventListener("pointerdown", leave)
    el.addEventListener("wheel", leave, { passive: true })
    return () => {
      el.removeEventListener("pointerdown", down, true)
      el.removeEventListener("pointerup", up, true)
      el.removeEventListener("pointermove", move)
      el.removeEventListener("pointerleave", leave)
      el.removeEventListener("pointerdown", leave)
      el.removeEventListener("wheel", leave)
      cancelAnimationFrame(frame)
      clearTimeout(hoverTimer)
    }
  }, [api, containerRef, open, isEditor, widenUpdates, apply])

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
  hovered: string | null,
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
  if (!appState.viewModeEnabled && !appState.editingTextElement && !appState.newElement && !appState.isResizing) {
    const card = soleSelectedCard(cards, Object.keys(appState.selectedElementIds))
    if (card) items.push({ id: card.frame.id, ref: card.ref, kind: "open", ...box(card) })
  }
  for (const card of cards) {
    const lookup = lookups.get(card.key)
    if (lookup?.status === "missing" || lookup?.status === "forbidden") {
      items.push({ id: card.frame.id, ref: card.ref, kind: lookup.status, ...box(card) })
    }
  }
  const card = hovered ? cards.find((c) => c.frame.id === hovered) : undefined
  const lookup = card ? lookups.get(card.key) : undefined
  // Only when the card shows less than the record says.
  const cut =
    lookup?.status === "ok" &&
    ((card?.title?.originalText ?? lookup.title) !== lookup.title ||
      (card?.detail?.originalText ?? lookup.detail) !== lookup.detail)
  if (card && lookup?.status === "ok" && cut && !appState.editingTextElement) {
    items.push({ id: card.frame.id, ref: card.ref, kind: "hover", title: lookup.title, detail: lookup.detail, ...box(card) })
  }
  return items
}

/** The card selected on its own, if one is: all of the selection is its
 * parts. */
function soleSelectedCard(cards: readonly RecordCard[], selection: readonly string[]): RecordCard | null {
  if (selection.length === 0) return null
  const card = cards.find((c) => selection.includes(c.frame.id))
  return card && selection.every((id) => card.partIds.includes(id)) ? card : null
}

function snapshotOf(card: RecordCard | null, elements: readonly ExcalidrawElement[]): CardSnapshot | null {
  if (!card) return null
  const parts = new Map<string, PartGeometry>()
  const badges = new Set<string>()
  for (const el of elements) {
    if (!card.partIds.includes(el.id)) continue
    parts.set(el.id, geometryOf(el))
    if (tagOf(el)?.part.startsWith("badge:")) badges.add(el.id)
  }
  const arrows = new Map<string, BoundArrow>()
  for (const bound of card.frame.boundElements ?? []) {
    if (bound.type !== "arrow") continue
    const el = elements.find((e) => e.id === bound.id) as ArrowElement | undefined
    if (!el || el.isDeleted) continue
    const arrow = arrowOf(el, card.frame.id)
    if (arrow.start || arrow.end) arrows.set(el.id, arrow)
  }
  return { frameId: card.frame.id, frame: elementBox(card.frame), parts, badges, arrows }
}

/** An arrow's points and which of its ends are bound to `frameId`. */
function arrowOf(el: ArrowElement, frameId: string): BoundArrow {
  return {
    x: el.x,
    y: el.y,
    points: el.points.map((p) => [p[0], p[1]] as const),
    elbowed: el.elbowed,
    start: el.startBinding?.elementId === frameId,
    end: el.endBinding?.elementId === frameId,
  }
}

type Box = { x: number; y: number; width: number; height: number }

/** `el` with new words fitted to `maxWidth`, or null when it already shows
 * them in a box their size. The box is checked too: Excalidraw draws text
 * clipped to its element's width, and a resize restores parts' boxes. */
function retext(el: ExcalidrawTextElement, words: string, maxWidth: number): ExcalidrawElement | null {
  const measure = (text: string) => measureText(el, text)
  const shown = el.originalText || el.text
  let text = shown
  if (!(shown === words && measure(shown).width <= maxWidth)) {
    const fitted = fitText(words, maxWidth, (t) => measure(t).width)
    // A cut of the record's words that fits and shows no less than this one
    // would (the one a card is placed with): keep it rather than recut it.
    const cut = shown.length > 1 && shown.endsWith("…") && words.startsWith(shown.slice(0, -1).trimEnd())
    if (!(cut && fitted.length <= shown.length && measure(shown).width <= maxWidth)) text = fitted
  }
  const size = measure(text)
  const sameBox = Math.abs(size.width - el.width) < 0.5 && Math.abs(size.height - el.height) < 0.5
  if (text === el.text && text === el.originalText && sameBox) return null
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

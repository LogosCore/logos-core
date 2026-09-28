// Remember and restore the reading position of a wiki page.
//
// The editor's scroll container is reused across documents, and between two
// documents it holds only a short loading skeleton, so without this every page
// opened at the top — reading two long pages alternately meant finding your
// place again on each switch. See lib/wiki-scroll-positions.ts for what is
// stored and why it is browser-local.

import { useEffect, useRef, type RefObject } from "react"
import type { Editor } from "@tiptap/core"
import {
  loadScrollPosition,
  saveScrollPosition,
  type WikiScrollPosition,
} from "@/lib/wiki-scroll-positions"

// Within this many pixels of the top counts as "at the top": saved as such, so
// the page reopens flush with its title rather than a few pixels down.
const TOP_SLOP = 4
// How long a restored position is held against content that finishes laying
// out afterwards (images, highlighted code, file previews above the anchor).
// Any input from the operator ends it sooner.
const PIN_MS = 2500
const WRITE_DELAY_MS = 250

/**
 * Where the viewport is, as the first top-level block whose bottom is below
 * the container's top edge and the distance into it. Null when the editor is
 * not mounted in this container — between documents the skeleton replaces it,
 * and a scroll event from that swap must not be recorded as a position.
 */
export function measurePosition(
  container: HTMLElement,
  editorDom: HTMLElement,
  documentId: string,
): WikiScrollPosition | null {
  if (!editorDom.isConnected || !container.contains(editorDom)) return null
  const scrollTop = container.scrollTop
  if (scrollTop <= TOP_SLOP) return { documentId, block: 0, offset: 0, scrollTop: 0 }

  const top = container.getBoundingClientRect().top
  const blocks = editorDom.children
  // Blocks are laid out top to bottom, so the first one still showing is found
  // by binary search — a long page has thousands and this runs on scroll.
  let lo = 0
  let hi = blocks.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (blocks[mid].getBoundingClientRect().bottom > top) {
      found = mid
      hi = mid - 1
    } else {
      lo = mid + 1
    }
  }
  if (found < 0) return { documentId, block: -1, offset: 0, scrollTop } // in the footer
  const offset = top - blocks[found].getBoundingClientRect().top
  return { documentId, block: found, offset, scrollTop }
}

/** Scroll so `position` is at the container's top edge again. */
export function applyPosition(
  container: HTMLElement,
  editorDom: HTMLElement,
  position: WikiScrollPosition,
) {
  if (position.scrollTop === 0) {
    container.scrollTop = 0
    return
  }
  const block = position.block >= 0 ? editorDom.children[position.block] : undefined
  if (!block) {
    container.scrollTop = position.scrollTop
    return
  }
  const top = container.getBoundingClientRect().top
  container.scrollTop += block.getBoundingClientRect().top - top + position.offset
}

export function useWikiScrollMemory({
  documentId,
  scrollRef,
  editor,
  isReady,
  enabled,
}: {
  documentId: string
  scrollRef: RefObject<HTMLElement | null>
  editor: Editor | null
  isReady: boolean
  enabled: boolean
}) {
  const restoredFor = useRef<string | null>(null)

  // Restore once per document open, as soon as its content is in the view.
  useEffect(() => {
    const container = scrollRef.current
    if (!enabled || !isReady || !editor || !container) return
    if (restoredFor.current === documentId) return
    restoredFor.current = documentId

    const editorDom = editor.view.dom as HTMLElement
    const saved = loadScrollPosition(documentId)
    if (!saved) {
      container.scrollTop = 0
      return
    }
    applyPosition(container, editorDom, saved)

    // Hold the position while late content settles above it, until the
    // operator does anything or the window runs out.
    let pinned = true
    const unpin = () => {
      if (!pinned) return
      pinned = false
      observer.disconnect()
      clearTimeout(timer)
      for (const type of INPUT_EVENTS) container.removeEventListener(type, unpin)
    }
    const observer = new ResizeObserver(() => {
      if (pinned) applyPosition(container, editorDom, saved)
    })
    observer.observe(editorDom)
    const timer = setTimeout(unpin, PIN_MS)
    for (const type of INPUT_EVENTS) container.addEventListener(type, unpin, { passive: true })
    return unpin
  }, [enabled, isReady, editor, documentId, scrollRef])

  // Record the position as the operator scrolls. Measured on the frame after
  // each scroll, written a moment later; the pending write is flushed when the
  // document changes or the page is left, because by the time the next
  // document's effects run the old content is already gone from the DOM.
  useEffect(() => {
    const container = scrollRef.current
    if (!enabled || !isReady || !editor || !container) return
    const editorDom = editor.view.dom as HTMLElement

    let frame = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    let pending: WikiScrollPosition | null = null

    const flush = () => {
      clearTimeout(timer)
      timer = undefined
      if (pending) saveScrollPosition(pending)
      pending = null
    }
    const onScroll = () => {
      if (frame) return
      frame = requestAnimationFrame(() => {
        frame = 0
        const position = measurePosition(container, editorDom, documentId)
        if (!position) return
        pending = position
        clearTimeout(timer)
        timer = setTimeout(flush, WRITE_DELAY_MS)
      })
    }

    container.addEventListener("scroll", onScroll, { passive: true })
    window.addEventListener("pagehide", flush)
    return () => {
      container.removeEventListener("scroll", onScroll)
      window.removeEventListener("pagehide", flush)
      cancelAnimationFrame(frame)
      flush()
    }
  }, [enabled, isReady, editor, documentId, scrollRef])
}

const INPUT_EVENTS = ["wheel", "touchstart", "pointerdown", "keydown"] as const

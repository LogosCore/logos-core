// Paints search matches on the open page with the CSS Custom Highlight API
// rather than <mark> elements: the body is a ProseMirror view, which owns its
// DOM and would undo (or be confused by) anything inserted into it. A
// highlight is a set of ranges the browser draws over text without touching
// the tree. Where the API is missing the page simply goes unhighlighted; the
// sidebar's results still mark their matches (viewer-highlight.tsx).

import { useEffect, type RefObject } from "react"
import { matchRanges } from "./viewer-data"

const HIGHLIGHT_NAME = "logos-search"

function paint(root: HTMLElement, words: string[]): Range | null {
  const ranges: Range[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
    for (const [start, end] of matchRanges(node.data, words)) {
      const range = document.createRange()
      range.setStart(node, start)
      range.setEnd(node, end)
      ranges.push(range)
    }
  }
  CSS.highlights.set(HIGHLIGHT_NAME, new Highlight(...ranges))
  return ranges[0] ?? null
}

/**
 * Paints every match of `words` under `root`, and scrolls the first into
 * view whenever the words or `scrollKey` (the page) change. Repaints when the
 * content changes underneath — chips render after the text around them — but
 * scrolls only once, so the reader is not yanked back while reading.
 */
export function usePageHighlight(root: RefObject<HTMLElement | null>, words: string[], scrollKey: string) {
  const key = words.join("\u0000")
  useEffect(() => {
    const el = root.current
    if (!el || typeof CSS === "undefined" || !("highlights" in CSS)) return
    if (words.length === 0) {
      CSS.highlights.delete(HIGHLIGHT_NAME)
      return
    }

    let scrolled = false
    let timer: number | undefined
    const run = () => {
      const first = paint(el, words)
      if (first && !scrolled) {
        scrolled = true
        first.startContainer.parentElement?.scrollIntoView({ block: "center" })
      }
    }
    // After the editor and its node views have rendered.
    timer = window.setTimeout(run, 0)
    const observer = new MutationObserver(() => {
      window.clearTimeout(timer)
      timer = window.setTimeout(run, 50)
    })
    observer.observe(el, { childList: true, subtree: true, characterData: true })
    return () => {
      observer.disconnect()
      window.clearTimeout(timer)
      CSS.highlights.delete(HIGHLIGHT_NAME)
    }
    // words is re-derived every render; `key` is its value.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root, key, scrollKey])
}

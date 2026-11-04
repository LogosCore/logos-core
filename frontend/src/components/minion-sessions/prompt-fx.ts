import { createContext, useContext, useSyncExternalStore } from "react"
import type { FxEngine, LineShape } from "./prompt-fx-engine"
import { editOf, type Point, type TextSpan } from "./prompt-fx-model"

// How a console's prompt reaches its effect (PromptFxLayer draws it): a
// handle that is there whether an effect is on or not, so the prompt tells
// it what happens and need not know. The prompt's points are measured as it
// happens, before the line changes; with no effect on, nothing is measured.

/** What the prompt tells its effect. */
export interface PromptFx {
  /**
   * input's line edited, before it what it was: a key typed or deleted, at
   * the caret; more than a letter pasted or cut, along the text it moved.
   */
  edited(input: HTMLInputElement, before: string): void
  /** A word completed (Tab): input's line becomes line, the word from start to end in it. */
  completed(input: HTMLInputElement, line: string, start: number, end: number): void
  /** A line recalled from the history (older: ↑): input's line becomes line. */
  recalled(input: HTMLInputElement, line: string, older: boolean): void
  /** input's line ran: struck at its end. */
  ran(input: HTMLInputElement): void
  /** input's line turned another effect on (/fx): struck by that one, once it is up. */
  switched(input: HTMLInputElement): void
  /** input's line does not read. */
  refused(input: HTMLInputElement): void
}

/** The layer's side of the handle: the effect drawing, or none. */
export interface PromptFxHandle extends PromptFx {
  attach(engine: FxEngine | null): void
}

// A line that turns an effect on (/fx inferno) is struck by it once it is
// up, if that is this soon: the effect it replaces, if any, goes first.
const REPLAY_MS = 1500

export function promptFxHandle(): PromptFxHandle {
  let engine: FxEngine | null = null
  let pending: { line: LineShape; when: number } | null = null
  return {
    edited(input, before) {
      if (!engine) return
      const after = input.value
      const e = editOf(before, after, input.selectionEnd ?? after.length)
      // What went, where it was; what came, where it is now.
      if (e.removed > 1) engine.cut(spanShape(input, before, e.start, e.start + e.removed))
      if (e.inserted > 1) engine.pasted(spanShape(input, after, e.start, e.start + e.inserted))
      else if (e.inserted === 1) engine.typed(caretPoint(input, false), false)
      else if (e.removed === 1) engine.typed(caretPoint(input, false), true)
      engine.lineIs(spanShape(input, after, 0, after.length))
    },
    completed(input, line, start, end) {
      if (!engine) return
      engine.completed(pointAt(input, line, start), pointAt(input, line, end))
      engine.lineIs(spanShape(input, line, 0, line.length))
    },
    recalled(input, line, older) {
      if (!engine) return
      engine.recalled(pointAt(input, line, 0), pointAt(input, line, line.length), older)
      engine.lineIs(spanShape(input, line, 0, line.length))
    },
    ran(input) {
      const line = lineShape(input)
      if (engine) {
        engine.ran(line)
        // Gone from the prompt: nothing left to crackle across.
        engine.lineIs(null)
      } else pending = { line, when: performance.now() }
    },
    switched(input) {
      pending = { line: lineShape(input), when: performance.now() }
    },
    refused(input) {
      engine?.refused(caretPoint(input, true))
    },
    attach(next) {
      engine = next
      // The effect going leaves the strike for the one coming.
      if (!next) return
      if (pending && performance.now() - pending.when < REPLAY_MS) next.ran(pending.line)
      pending = null
    },
  }
}

let measure: CanvasRenderingContext2D | null = null

const fontOf = (cs: CSSStyleDeclaration) => `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`

/**
 * Where in the page the point before text's index is, text in input (or
 * about to be: a completion's line, set after): text up to there measured
 * in input's font, less what it has scrolled, kept inside the input.
 */
function pointAt(input: HTMLInputElement, text: string, index: number): Point {
  const r = input.getBoundingClientRect()
  const cs = getComputedStyle(input)
  measure ??= document.createElement("canvas").getContext("2d")
  let width = 0
  if (measure) {
    measure.font = fontOf(cs)
    width = measure.measureText(text.slice(0, index)).width
  }
  const left = r.left + (parseFloat(cs.paddingLeft) || 0) + (parseFloat(cs.borderLeftWidth) || 0)
  return { x: Math.min(r.right - 2, Math.max(r.left, left + width - input.scrollLeft)), y: r.top + r.height / 2 }
}

/**
 * text's letters from up to to laid out as input draws them (text in it,
 * or as it was: what a cut took): where each starts and where they end,
 * the input's middle and edges. Each letter measured once (the console's
 * font is monospace: no kerning to miss).
 */
function spanShape(input: HTMLInputElement, text: string, from: number, to: number): TextSpan {
  const r = input.getBoundingClientRect()
  const cs = getComputedStyle(input)
  measure ??= document.createElement("canvas").getContext("2d")
  if (measure) measure.font = fontOf(cs)
  const origin = r.left + (parseFloat(cs.paddingLeft) || 0) + (parseFloat(cs.borderLeftWidth) || 0) - input.scrollLeft
  const xs = [origin + (measure && from > 0 ? measure.measureText(text.slice(0, from)).width : 0)]
  const part = text.slice(from, to)
  const widths = new Map<string, number>()
  for (const ch of part) {
    let w = widths.get(ch)
    if (w === undefined) {
      w = measure ? measure.measureText(ch).width : 0
      widths.set(ch, w)
    }
    // A letter outside the BMP is two code units: the second starts where the first does.
    for (let k = 0; k < ch.length; k++) xs.push(xs[xs.length - 1] + (k === ch.length - 1 ? w : 0))
  }
  return { text: part, xs, y: r.top + r.height / 2, left: r.left, right: r.right }
}

/** input's line as it stands, laid out as the input draws it, with its font and color. */
function lineShape(input: HTMLInputElement): LineShape {
  const cs = getComputedStyle(input)
  const span = spanShape(input, input.value, 0, input.value.length)
  return {
    ...span,
    font: fontOf(cs),
    size: parseFloat(cs.fontSize) || 12,
    color: cs.color,
    end: { x: Math.min(span.right - 2, Math.max(span.left, span.xs[span.xs.length - 1])), y: span.y },
  }
}

/** Where in the page input's caret is (end: where its text ends). */
export function caretPoint(input: HTMLInputElement, end: boolean): Point {
  const v = input.value
  return pointAt(input, v, end ? v.length : (input.selectionEnd ?? v.length))
}

export const PromptFxContext = createContext<PromptFx | null>(null)

/** The prompt's effect handle; null outside a console. */
export function usePromptFx(): PromptFx | null {
  return useContext(PromptFxContext)
}

const REDUCED = "(prefers-reduced-motion: reduce)"

/** Whether the system asks for less motion: then no effect shows. */
export function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(REDUCED).matches
}

function onReducedChange(changed: () => void) {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return () => {}
  const m = window.matchMedia(REDUCED)
  m.addEventListener("change", changed)
  return () => m.removeEventListener("change", changed)
}

/** prefersReducedMotion, kept current as the setting changes. */
export function useReducedMotion(): boolean {
  return useSyncExternalStore(onReducedChange, prefersReducedMotion, () => false)
}

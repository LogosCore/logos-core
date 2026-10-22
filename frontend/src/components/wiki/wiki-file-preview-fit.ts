// How tall a line-based preview needs to be: a text file, JSON, YAML, a
// script. Without this a three-line file opens in a frame sized to the
// viewport and reads as a grey page with a sentence at the top.
//
// The frame is sandboxed without allow-same-origin, so the page cannot look
// inside it and measure. It does not need to: these previews are one <pre>
// built by our own converters, so the line count is known and the type
// metrics are the stylesheet's (wiki-file-preview-document.ts). The result
// caps the frame's height rather than setting it, so a short estimate costs a
// scrollbar inside the frame, never a hidden line.

/** The <pre>'s metrics, from the preview stylesheet: 14px body type at 0.92em,
 *  line-height 1.65, 12px padding top and bottom. */
const LINE_HEIGHT_PX = 14 * 0.92 * 1.65
const VERTICAL_PADDING_PX = 24
const HORIZONTAL_PADDING_PX = 24
/** Wider than most monospace faces' 0.6em advance, so wrapping is
 *  overestimated, which errs towards a frame a little too tall. */
const CHAR_WIDTH_PX = 14 * 0.92 * 0.62
/** Room for a horizontal scrollbar under lines that do not wrap, and for the
 *  truncation notice above the listing when there is one. */
const SCROLLBAR_PX = 18
const NOTICE_PX = 64
/** One line of slack for everything the estimate cannot know. */
const SLACK_PX = LINE_HEIGHT_PX

/** Past this many lines the frame is viewport-sized anyway. */
const MAX_MEASURED_LINES = 2_000

export interface TextLayout {
  /** Character count of each line, up to MAX_MEASURED_LINES of them. */
  lines: readonly number[]
  /** The lines wrap at the frame's width (plain text) instead of scrolling. */
  wraps: boolean
  /** A notice sits above the listing. */
  notice: boolean
}

/**
 * The layout of a preview body that is a single <pre>, or null for anything
 * else (a table, a document, a note), whose height is not ours to predict.
 */
export function textLayoutOf(bodyHtml: string, notice: boolean): TextLayout | null {
  const m = /^<pre(?: class="([^"]*)")?>([\s\S]*)<\/pre>$/.exec(bodyHtml)
  if (!m || m[2].includes("<pre")) return null
  // Tags are markup the converters added (highlighting, line numbers); an
  // entity is one character on screen.
  const text = m[2]
    // A script listing's lines are blocks with nothing between them.
    .replace(/<\/span><span class="script-line">/g, "</span>\n<span class=\"script-line\">")
    .replace(/<[^>]*>/g, "")
    .replace(/&(?:#\d+|#x[0-9a-fA-F]+|\w+);/g, "x")
  const lines = text.replace(/\n$/, "").split("\n", MAX_MEASURED_LINES).map((line) => line.length)
  return { lines, wraps: (m[1] ?? "").split(" ").includes("text-pre"), notice }
}

/** The frame height, in pixels, that shows all of layout at frameWidth. */
export function estimateTextHeight(layout: TextLayout, frameWidth: number): number {
  const perLine = Math.max(1, Math.floor((frameWidth - HORIZONTAL_PADDING_PX) / CHAR_WIDTH_PX))
  const rows = layout.wraps
    ? layout.lines.reduce((sum, length) => sum + Math.max(1, Math.ceil(length / perLine)), 0)
    : layout.lines.length
  return Math.ceil(
    VERTICAL_PADDING_PX +
      rows * LINE_HEIGHT_PX +
      (layout.wraps ? 0 : SCROLLBAR_PX) +
      (layout.notice ? NOTICE_PX : 0) +
      SLACK_PX,
  )
}

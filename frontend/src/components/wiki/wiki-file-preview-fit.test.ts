import { describe, expect, it } from "vitest"

import { estimateTextHeight, textLayoutOf } from "./wiki-file-preview-fit"
import { renderScriptPreview } from "./wiki-file-preview-script"
import { renderTextPreview } from "./wiki-file-preview-text"

describe("textLayoutOf", () => {
  it("measures a plain-text listing, which wraps", () => {
    const { bodyHtml } = renderTextPreview("short\n<a long line & more>\n")
    expect(textLayoutOf(bodyHtml, false)).toEqual({ lines: [5, 20], wraps: true, notice: false })
  })

  it("counts a script's lines, which are blocks with nothing between them", () => {
    const { bodyHtml } = renderScriptPreview("#!/bin/sh\necho hi\nexit 0")
    const layout = textLayoutOf(bodyHtml, false)
    expect(layout?.lines).toHaveLength(3)
    expect(layout?.wraps).toBe(false)
  })

  it("declines anything that is not one listing", () => {
    expect(textLayoutOf("<table></table>", false)).toBeNull()
    expect(textLayoutOf("<p>prose</p>", false)).toBeNull()
    expect(textLayoutOf("<pre>a</pre><pre>b</pre>", false)).toBeNull()
  })
})

describe("estimateTextHeight", () => {
  it("is a few lines tall for a few lines", () => {
    const three = estimateTextHeight({ lines: [10, 10, 10], wraps: false, notice: false }, 800)
    expect(three).toBeGreaterThan(80)
    expect(three).toBeLessThan(160)
  })

  it("grows as a long line wraps in a narrower frame", () => {
    const layout = { lines: [600], wraps: true, notice: false }
    expect(estimateTextHeight(layout, 400)).toBeGreaterThan(estimateTextHeight(layout, 1200))
  })

  it("leaves room for the notice above a truncated listing", () => {
    const lines = [10]
    expect(estimateTextHeight({ lines, wraps: false, notice: true }, 800)).toBeGreaterThan(
      estimateTextHeight({ lines, wraps: false, notice: false }, 800),
    )
  })
})

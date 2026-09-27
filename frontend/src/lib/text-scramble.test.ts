import { describe, expect, it } from "vitest"
import { scrambleAt } from "./text-scramble"

const zero = () => 0

describe("scrambleAt", () => {
  it("returns the target when finished", () => {
    expect(scrambleAt("Logos", 1)).toBe("Logos")
    expect(scrambleAt("Logos", 1.5)).toBe("Logos")
  })

  it("is fully scrambled at the start but keeps spaces and length", () => {
    // Two words on purpose. The wordmark this drives is one word today, but
    // preserving spaces is part of the function's contract — it is what keeps
    // the word shape legible while the glyphs churn — and a single-word input
    // cannot check it. With `zero` for random, every scrambled slot is GLYPHS[0].
    const target = "Logos Core"
    const out = scrambleAt(target, 0, zero)

    expect(out).toHaveLength(target.length)
    expect(out[5]).toBe(" ")
    expect(out).toBe("!!!!! !!!!")
    expect(out).not.toBe(target)
  })

  it("resolves characters from the left as progress grows", () => {
    const mid = scrambleAt("ABCDEFGH", 0.65, zero)
    expect(mid.startsWith("ABCD")).toBe(true)
    expect(mid.slice(4)).toBe("!!!!")
  })

  it("stays scrambled through the settle delay", () => {
    expect(scrambleAt("ABCD", 0.1, zero)).toBe("!!!!")
  })
})

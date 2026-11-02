import { describe, expect, it } from "vitest"
import { rateText } from "./file-overview-values"

describe("rateText", () => {
  it("says the rate and the time left", () => {
    expect(rateText(1024, 30 * 1024)).toBe(" · 1.0 KB/s · 30s left")
    expect(rateText(1024, 90 * 1024)).toBe(" · 1.0 KB/s · 1m 30s left")
  })
  it("says nothing without a rate", () => {
    expect(rateText(null, 100)).toBe("")
    expect(rateText(0, 100)).toBe("")
  })
})

import { describe, expect, test } from "vitest"
import { checkSize, hasTransparency, MAX_DECODED_PIXELS } from "@/components/wiki/wiki-image-decode-protocol"

describe("checkSize", () => {
  test("accepts a camera photo and refuses what would not fit in memory", () => {
    expect(() => checkSize(4032, 3024)).not.toThrow()
    expect(() => checkSize(MAX_DECODED_PIXELS, 2)).toThrow(/too large/)
    expect(() => checkSize(0, 100)).toThrow(/no size/)
  })
})

describe("hasTransparency", () => {
  test("is true only when some pixel is not opaque", () => {
    expect(hasTransparency(new Uint8ClampedArray([1, 2, 3, 255, 4, 5, 6, 255]))).toBe(false)
    expect(hasTransparency(new Uint8ClampedArray([1, 2, 3, 255, 4, 5, 6, 128]))).toBe(true)
  })
})

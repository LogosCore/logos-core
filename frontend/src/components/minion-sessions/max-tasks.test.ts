import { describe, expect, it } from "vitest"
import { parseMaxTasks } from "./max-tasks-values"

describe("parseMaxTasks", () => {
  it("takes a whole number 0 to 64", () => {
    expect(parseMaxTasks(" 0 ")).toEqual({ ok: true, value: 0 })
    expect(parseMaxTasks("64")).toEqual({ ok: true, value: 64 })
    expect(parseMaxTasks("65")).toEqual({ ok: false, error: "64 at most" })
    expect(parseMaxTasks("2.5").ok).toBe(false)
    expect(parseMaxTasks("-1").ok).toBe(false)
    expect(parseMaxTasks("").ok).toBe(false)
  })
})

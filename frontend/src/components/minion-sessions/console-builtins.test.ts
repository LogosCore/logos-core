import { describe, expect, it } from "vitest"
import { builtinCompletion, builtinNotice, isBuiltinLine, parseBuiltin } from "./console-builtins"

describe("the console's own commands", () => {
  it("are the lines that start with /", () => {
    expect(isBuiltinLine("/fx")).toBe(true)
    expect(isBuiltinLine("  /fx lightning")).toBe(true)
    expect(isBuiltinLine("ls /etc")).toBe(false)
    expect(isBuiltinLine("")).toBe(false)
  })

  it("/fx shows the effect on, sets one, or turns it off", () => {
    expect(parseBuiltin("/fx", "lightning")).toMatchObject({ ok: true, run: { id: "fx", set: undefined } })
    expect(parseBuiltin("/fx lightning", null)).toMatchObject({ ok: true, run: { id: "fx", set: "lightning" } })
    expect(parseBuiltin(" /FX  Lightning ", null)).toMatchObject({ ok: true, run: { id: "fx", set: "lightning" } })
    expect(parseBuiltin("/fx off", "lightning")).toMatchObject({ ok: true, run: { id: "fx", set: null } })
    expect(parseBuiltin("/fx inferno", "lightning")).toMatchObject({ ok: true, run: { id: "fx", set: "inferno" } })
  })

  it("says what is wrong, and whether the first word names one", () => {
    expect(parseBuiltin("/fx storm", null)).toEqual({ ok: false, error: 'No effect "storm": lightning, inferno, off', known: true })
    expect(parseBuiltin("/fx lightning off", null)).toMatchObject({ ok: false, known: true })
    expect(parseBuiltin("/f", null)).toMatchObject({ ok: false, known: false })
    expect(parseBuiltin("/", null)).toMatchObject({ ok: false, known: false })
  })

  it("says what it did", () => {
    expect(builtinNotice({ id: "fx", set: "lightning" }, null, false)).toBe("The prompt's effect: lightning, yours in every session.")
    expect(builtinNotice({ id: "fx", set: null }, "lightning", false)).toBe("The prompt has no effect now.")
    expect(builtinNotice({ id: "fx", set: undefined }, null, false)).toBe("The prompt has no effect. /fx lightning turns lightning on.")
    expect(builtinNotice({ id: "fx", set: undefined }, "lightning", true)).toMatch(/less motion/)
  })

  it("offers the commands while the first word is typed, then their values", () => {
    expect(builtinCompletion("/")).toEqual({ start: 0, options: [expect.objectContaining({ text: "/fx" })] })
    expect(builtinCompletion("/f")?.options.map((o) => o.text)).toEqual(["/fx"])
    expect(builtinCompletion("/fx")).toBeNull()
    expect(builtinCompletion("/fx ")?.options.map((o) => o.text)).toEqual(["lightning", "inferno", "off"])
    expect(builtinCompletion("/fx ")?.start).toBe(4)
    expect(builtinCompletion("/fx i")?.options.map((o) => o.text)).toEqual(["inferno"])
    expect(builtinCompletion("/fx l")?.options.map((o) => o.text)).toEqual(["lightning"])
    expect(builtinCompletion("/fx lightning")).toBeNull()
    expect(builtinCompletion("/zz ")).toBeNull()
    expect(builtinCompletion("/fx lightning ")).toBeNull()
  })
})

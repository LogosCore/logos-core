import { describe, expect, test, vi } from "vitest"
import type { FormEvent } from "react"
import { fromWithin, keepSubmitIn } from "./portal-events"

describe("a popup's events", () => {
  test("a form in it submits itself alone, its own handler first", () => {
    const order: string[] = []
    const e = { stopPropagation: vi.fn(() => order.push("stopped")) } as unknown as FormEvent<HTMLDivElement>
    keepSubmitIn<FormEvent<HTMLDivElement>>(() => order.push("its own"))(e)
    expect(order).toEqual(["its own", "stopped"])
    keepSubmitIn<FormEvent<HTMLDivElement>>()(e)
    expect(e.stopPropagation).toHaveBeenCalledTimes(2)
  })

  test("a key from a popup opened from an element is not the element's", () => {
    const inner = {} as EventTarget
    const portal = {} as EventTarget
    const form = { contains: (n: unknown) => n === inner } as unknown as EventTarget
    expect(fromWithin({ currentTarget: form, target: inner })).toBe(true)
    expect(fromWithin({ currentTarget: form, target: portal })).toBe(false)
  })
})

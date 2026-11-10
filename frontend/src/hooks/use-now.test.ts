import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { clockOf } from "./use-now"

describe("a shared clock", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(1_000_000)
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it("ticks every reader at once, on one timer", () => {
    // An interval of its own per test: the clocks live for the module's life.
    const clock = clockOf(1_001)
    const seen: number[][] = [[], []]
    const stop = [0, 1].map((i) => clock.subscribe(() => seen[i].push(clock.read())))
    expect(vi.getTimerCount()).toBe(1)
    vi.advanceTimersByTime(1_001)
    vi.advanceTimersByTime(1_001)
    expect(seen).toEqual([
      [1_001_001, 1_002_002],
      [1_001_001, 1_002_002],
    ])
    stop.forEach((s) => s())
  })

  it("stops when nothing reads it, and starts again with the next reader", () => {
    const clock = clockOf(1_002)
    const stop = clock.subscribe(() => {})
    stop()
    expect(vi.getTimerCount()).toBe(0)
    const again = clock.subscribe(() => {})
    expect(vi.getTimerCount()).toBe(1)
    again()
  })

  it("reads the time now when stopped and a tick behind, the same however often it is read", () => {
    const clock = clockOf(1_003)
    const first = clock.read()
    vi.advanceTimersByTime(5_000)
    const later = clock.read()
    expect(later).toBe(first + 5_000)
    vi.advanceTimersByTime(10)
    // Under a tick behind: the same value, as a render reading it twice needs.
    expect(clock.read()).toBe(later)
  })

  it("reads the last tick between ticks while running", () => {
    const clock = clockOf(1_004)
    const stop = clock.subscribe(() => {})
    vi.advanceTimersByTime(1_004)
    const tick = clock.read()
    vi.advanceTimersByTime(500)
    expect(clock.read()).toBe(tick)
    stop()
  })

  it("keeps a clock per interval", () => {
    expect(clockOf(1_005)).toBe(clockOf(1_005))
    expect(clockOf(1_005)).not.toBe(clockOf(1_006))
  })
})

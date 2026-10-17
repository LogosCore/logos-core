import { describe, expect, test } from "vitest"

import { createThrottle, type Clock } from "./drawing-throttle"

/** A clock the test moves by hand. */
function fakeClock() {
  let now = 0
  let timers: { at: number; fn: () => void; handle: number }[] = []
  let next = 1
  const clock: Clock = {
    now: () => now,
    setTimeout(fn, ms) {
      const handle = next++
      timers.push({ at: now + ms, fn, handle })
      return handle
    },
    clearTimeout(handle) {
      timers = timers.filter((t) => t.handle !== handle)
    },
  }
  const advance = (ms: number) => {
    now += ms
    for (const t of timers.filter((t) => t.at <= now)) {
      timers = timers.filter((x) => x !== t)
      t.fn()
    }
  }
  return { clock, advance, pending: () => timers.length }
}

describe("createThrottle", () => {
  test("the first change of a gesture goes out at once", () => {
    const { clock } = fakeClock()
    let runs = 0
    createThrottle(() => runs++, 50, clock).request()
    expect(runs).toBe(1)
  })

  // A 60 fps gesture lasting one second: 20 writes, not 60.
  test("changes on every frame go out at most once per interval", () => {
    const { clock, advance } = fakeClock()
    let runs = 0
    const t = createThrottle(() => runs++, 50, clock)
    for (let frame = 0; frame < 60; frame++) {
      t.request()
      advance(1000 / 60)
    }
    advance(50)
    expect(runs).toBeGreaterThanOrEqual(19)
    expect(runs).toBeLessThanOrEqual(21)
  })

  // The last frame of a gesture must not be dropped, only delayed.
  test("a change inside the interval is sent once it has passed", () => {
    const { clock, advance } = fakeClock()
    let runs = 0
    const t = createThrottle(() => runs++, 50, clock)
    t.request()
    advance(10)
    t.request()
    expect(runs).toBe(1)
    advance(40)
    expect(runs).toBe(2)
  })

  test("an urgent change goes out at once and replaces the wait", () => {
    const { clock, advance, pending } = fakeClock()
    let runs = 0
    const t = createThrottle(() => runs++, 50, clock)
    t.request()
    advance(10)
    t.request()
    t.request(true)
    expect(runs).toBe(2)
    expect(pending()).toBe(0)
  })

  test("flush sends a waiting change, and does nothing when none waits", () => {
    const { clock, advance } = fakeClock()
    let runs = 0
    const t = createThrottle(() => runs++, 50, clock)
    t.flush()
    expect(runs).toBe(0)
    t.request()
    advance(5)
    t.request()
    t.flush()
    expect(runs).toBe(2)
  })

  test("cancel drops a waiting change", () => {
    const { clock, advance } = fakeClock()
    let runs = 0
    const t = createThrottle(() => runs++, 50, clock)
    t.request()
    t.request()
    t.cancel()
    advance(100)
    expect(runs).toBe(1)
  })
})

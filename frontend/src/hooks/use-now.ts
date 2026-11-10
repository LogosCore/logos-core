import { useSyncExternalStore } from "react"

// One clock per interval, shared by every component on it: they read the
// same time and re-render in the same tick, one render for all rather than
// one each (a table's time cells, a modal's cards), and the clock runs only
// while something reads it. A component that mounts between ticks reads the
// last tick's time, as its neighbours show it; one that mounts when nothing
// else reads the clock reads the time then.

interface Clock {
  subscribe: (listener: () => void) => () => void
  read: () => number
}

const clocks = new Map<number, Clock>()

/** The clock that ticks every intervalMs (made on first use, kept for the page's life). */
export function clockOf(intervalMs: number): Clock {
  const known = clocks.get(intervalMs)
  if (known) return known
  let now = Date.now()
  let timer: ReturnType<typeof setInterval> | null = null
  const listeners = new Set<() => void>()
  const clock: Clock = {
    subscribe(listener) {
      listeners.add(listener)
      timer ??= setInterval(() => {
        now = Date.now()
        for (const l of listeners) l()
      }, intervalMs)
      return () => {
        listeners.delete(listener)
        if (listeners.size === 0 && timer !== null) {
          clearInterval(timer)
          timer = null
        }
      }
    },
    read() {
      // Stopped, and a tick or more behind: the time now, kept until the
      // clock ticks (a render reads the same value however often it asks).
      if (timer === null && Date.now() - now >= intervalMs) now = Date.now()
      return now
    },
  }
  clocks.set(intervalMs, clock)
  return clock
}

/**
 * The time now, in ms, refreshed every intervalMs while mounted: for live
 * durations, which render may not read off the clock itself.
 */
export function useNow(intervalMs = 1000): number {
  const clock = clockOf(intervalMs)
  return useSyncExternalStore(clock.subscribe, clock.read)
}

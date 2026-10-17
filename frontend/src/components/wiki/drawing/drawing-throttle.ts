// Rate limits for what a drawing canvas sends while the pointer is moving.
//
// Excalidraw calls onChange on every frame of a gesture, and every frame bumps
// the version of whatever is under the pointer. Writing each one to the room
// made every frame a Y.js update and a WebSocket message to the server and to
// every peer, each carrying the whole element. A freehand stroke re-sends its
// entire point list on each of those, so its cost grows with the square of
// its length. Measured against real Y.js encoding: a 5 s stroke sent about
// 1 MB, and dragging 20 shapes for 2 s about 850 KB. Every peer also re-merged
// and re-sorted the whole scene once per message.
//
// What this does instead: the first change of a gesture goes out at once,
// later ones at most every `intervalMs`, and the change that ends the gesture
// at once, so the final state never waits. The run always reads the latest
// state at the moment it fires rather than a copy captured when it was
// requested, so a delayed write can never publish something staler than what
// is on the canvas.

/** Writes to the room while a gesture is in progress: 20 a second. Peers still
 * watch a stroke grow smoothly, and a gesture sends about a third of the
 * bytes it did (the 5 s stroke: 1 MB to 340 KB). */
export const SCENE_WRITE_INTERVAL_MS = 50

/** Cursor broadcasts. 30 a second, the same rate as Excalidraw's own
 * collaboration mode (CURSOR_SYNC_TIMEOUT in its excalidraw-app). */
export const POINTER_WRITE_INTERVAL_MS = 33

export interface Clock {
  now(): number
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
}

const realClock: Clock = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
}

export interface Throttle {
  /** Ask for a run. `urgent` runs now, cancelling any wait; otherwise it runs
   * now if the interval has passed since the last run, or once it has. */
  request(urgent?: boolean): void
  /** Run now if a run is waiting. For leaving the page and unmounting. */
  flush(): void
  /** Drop a waiting run. */
  cancel(): void
}

export function createThrottle(run: () => void, intervalMs: number, clock: Clock = realClock): Throttle {
  let last = -Infinity
  let timer: unknown = null

  const fire = () => {
    if (timer !== null) clock.clearTimeout(timer)
    timer = null
    last = clock.now()
    run()
  }

  return {
    request(urgent = false) {
      if (urgent) return fire()
      // A run is already waiting, and it will read whatever is current then.
      if (timer !== null) return
      const wait = last + intervalMs - clock.now()
      if (wait <= 0) return fire()
      timer = clock.setTimeout(fire, wait)
    },
    flush() {
      if (timer !== null) fire()
    },
    cancel() {
      if (timer !== null) clock.clearTimeout(timer)
      timer = null
    },
  }
}

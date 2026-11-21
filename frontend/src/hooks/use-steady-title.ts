import { useState } from "react"

// A browser shows an element's title as its tooltip, and draws the tooltip
// again whenever that title changes under the pointer. Chrome reads it again
// on any layout of the page, so a title that counts (a check-in's age, a
// running step's time) or follows live data blinks whenever anything else on
// the page changes: another row's Last seen moving on, a check-in. A steady
// title stays as it was when the pointer came onto its element until the
// pointer leaves; the next hover reads the values of then.

interface Held {
  of: string | undefined
  title: string | undefined
}

/**
 * An element's title, held while the pointer is on it: the first one it
 * shows then stays until the pointer leaves. Spread the result on the
 * element. of names what the title is about (a row's id) where a list may
 * give the element another row under a pointer that stays (a table that keys
 * its rows by place): that row's title is held from then.
 */
export function useSteadyTitle(title: string | undefined, of?: string) {
  const [held, setHeld] = useState<Held | null>(null)
  // Another row under the pointer, or a title where there was none: held
  // from now (state adjusted while rendering, as React has it for state from
  // props).
  if (held !== null && (held.of !== of || (held.title === undefined && title !== undefined))) setHeld({ of, title })
  return {
    title: held !== null && held.of === of ? (held.title ?? title) : title,
    onPointerEnter: () => setHeld({ of, title }),
    onPointerLeave: () => setHeld(null),
  }
}

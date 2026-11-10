import { useSyncExternalStore } from "react"

const MOBILE_BREAKPOINT = 768
const QUERY = `(max-width: ${MOBILE_BREAKPOINT - 1}px)`

function subscribe(onChange: () => void) {
  const mql = window.matchMedia(QUERY)
  mql.addEventListener("change", onChange)
  return () => mql.removeEventListener("change", onChange)
}

const isMobileNow = () => window.innerWidth < MOBILE_BREAKPOINT

/**
 * Whether the window is narrower than the mobile breakpoint, read from the
 * first render on (an external store: no render as desktop first, then a
 * second one as mobile) and again as the width crosses it.
 */
export function useIsMobile() {
  return useSyncExternalStore(subscribe, isMobileNow)
}

import { useEffect, useState } from "react"

/**
 * How long a load runs before its placeholders show. A quick load (most of
 * them) leaves its table empty for that moment instead of flashing them; a
 * load that hangs shows them.
 */
export const PLACEHOLDER_DELAY_MS = 500

/**
 * flag, once it has been true for delayMs; false again as soon as flag is.
 * What a loading placeholder waits on.
 */
export function useDelayedFlag(flag: boolean, delayMs: number): boolean {
  const [elapsed, setElapsed] = useState(false)
  useEffect(() => {
    if (!flag) return
    const timer = setTimeout(() => setElapsed(true), delayMs)
    return () => {
      clearTimeout(timer)
      setElapsed(false)
    }
  }, [flag, delayMs])
  return flag && elapsed
}

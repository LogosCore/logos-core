import { useEffect, useState } from "react"

/**
 * The time now, in ms, refreshed every intervalMs while mounted: for live
 * durations, which render may not read off the clock itself.
 */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])
  return now
}

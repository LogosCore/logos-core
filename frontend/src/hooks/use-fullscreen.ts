import { useEffect, useState, type RefObject } from "react"

/**
 * Full screen for an element: whether it is (the browser says, Esc
 * included), and a toggle that enters it, or leaves it when it is.
 */
export function useFullscreen(ref: RefObject<HTMLElement | null>): { full: boolean; toggle: () => void } {
  const [full, setFull] = useState(false)
  useEffect(() => {
    const onChange = () => setFull(ref.current !== null && document.fullscreenElement === ref.current)
    document.addEventListener("fullscreenchange", onChange)
    return () => document.removeEventListener("fullscreenchange", onChange)
  }, [ref])
  const toggle = () => {
    if (ref.current && document.fullscreenElement === ref.current) void document.exitFullscreen?.()
    else void ref.current?.requestFullscreen?.()
  }
  return { full, toggle }
}

import { useEffect, useRef, useState } from "react"

/** The id of a form section, from its title: "Dummy builds" → "section-dummy-builds". */
export function sectionId(title: string): string {
  return `section-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`
}

/** How far below the viewport top a section's heading counts as reached. */
const SPY_OFFSET = 120

/**
 * The section the reader is in, and a way to go to one. Reading position: the
 * last section whose top has scrolled above SPY_OFFSET, or the last section
 * once the page is scrolled to its end (short last sections never reach the
 * top). A section picked from the list stays highlighted until the reader
 * scrolls themselves: near the end of the page it may not move at all.
 */
export function useActiveSection(ids: string[]): [string | null, (id: string) => void] {
  const [active, setActive] = useState<string | null>(ids[0] ?? null)
  const idsRef = useRef(ids)
  const pickedRef = useRef(false)
  useEffect(() => {
    idsRef.current = ids
  })

  useEffect(() => {
    const update = () => {
      if (pickedRef.current) return
      const els = idsRef.current
        .map((id) => document.getElementById(id))
        .filter((el): el is HTMLElement => el !== null)
      if (els.length === 0) return
      const last = els[els.length - 1]
      // At the end: the last section has fully come into view.
      if (last.getBoundingClientRect().bottom <= window.innerHeight + 1) {
        setActive(last.id)
        return
      }
      let current = els[0].id
      for (const el of els) {
        if (el.getBoundingClientRect().top <= SPY_OFFSET) current = el.id
      }
      setActive(current)
    }
    // The reader taking over ends a pick.
    const release = () => {
      pickedRef.current = false
    }
    // Capture: the page scrolls inside the app layout, not the window.
    document.addEventListener("scroll", update, { capture: true, passive: true })
    window.addEventListener("resize", update)
    for (const ev of ["wheel", "touchstart", "keydown"] as const) {
      window.addEventListener(ev, release, { passive: true })
    }
    update()
    return () => {
      document.removeEventListener("scroll", update, { capture: true })
      window.removeEventListener("resize", update)
      for (const ev of ["wheel", "touchstart", "keydown"] as const) {
        window.removeEventListener(ev, release)
      }
    }
  }, [])

  const goTo = (id: string) => {
    pickedRef.current = true
    setActive(id)
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" })
  }
  return [active, goTo]
}

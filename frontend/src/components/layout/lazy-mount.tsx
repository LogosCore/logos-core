import { useState, type ReactNode } from "react"
import { useLoaded, type Preloadable } from "@/lib/preloadable"

/**
 * Mounts what code exports once `when` first turns true, and keeps it mounted
 * from then on, so a dialog plays its closing animation and keeps its state
 * between openings. Its code loads then (or earlier, through code.load()):
 * the app shell renders the trigger on every page, the dialog only when
 * someone asks for it.
 */
export function LazyMount<T>({
  code,
  when,
  children,
}: {
  code: Preloadable<T>
  when: boolean
  children: (loaded: T) => ReactNode
}) {
  const [wanted, setWanted] = useState(when)
  if (when && !wanted) setWanted(true)
  return wanted ? <Loaded code={code}>{children}</Loaded> : null
}

function Loaded<T>({ code, children }: { code: Preloadable<T>; children: (loaded: T) => ReactNode }) {
  const loaded = useLoaded(code)
  return loaded ? children(loaded) : null
}

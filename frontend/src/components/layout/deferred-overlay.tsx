import {
  Suspense,
  lazy,
  useEffect,
  useState,
  type ComponentType,
} from "react"

// AppLayout mounts a row of global overlays — task dialogs, the wiki palette,
// the task picker. Every one of them renders nothing until a store flag says it
// is open, but a static import is not conditional: their modules were part of
// the shell's graph, so first paint parsed all of them, on every page, for
// every user, including the login screen that can open none of them.
//
// deferOverlay() closes that gap. The wrapper subscribes to the open flag and
// nothing else, so the module is fetched the first time the overlay is actually
// wanted.
//
// Two details that are easy to get wrong:
//
//   - The flag is read by the wrapper, not by AppLayout. Reading it in the
//     layout would subscribe the whole shell — sidebar, Outlet and all — to
//     every dialog open, and with no memoization in the tree that re-renders
//     the entire page. Today only the dialog re-renders; keep it that way.
//
//   - Once mounted, the overlay stays mounted. base-ui keeps a closing Popup
//     in the DOM for ~100ms to fade it out, and several of these dialogs carry
//     their last config through that window on purpose (see
//     wiki-command-palette's `lastConfig`). Unmounting the moment the flag goes
//     false would cut the exit animation at t=0 and blink the page.
export function deferOverlay<K extends string>(
  load: () => Promise<Record<K, ComponentType>>,
  name: K,
  useIsOpen: () => boolean,
  options: { warm?: boolean } = {},
): ComponentType {
  // The cast is the price of keeping `name` checked against the module's real
  // exports: through the generic key, `lazy` infers
  // LazyExoticComponent<Record<K, ComponentType>[K]>, whose props JSX cannot
  // reduce to anything an empty props object satisfies. Every overlay here
  // takes no props, so ComponentType is the accurate type — and the generic
  // still rejects a name the module does not export, which is the mistake
  // worth catching.
  const Overlay = lazy(() =>
    load().then((m) => ({ default: m[name] })),
  ) as unknown as ComponentType
  const { warm = false } = options

  return function DeferredOverlay() {
    const isOpen = useIsOpen()

    // Latches on the first open and never lets go — see the note above.
    const [everOpened, setEverOpened] = useState(false)
    if (isOpen && !everOpened) setEverOpened(true)

    // `warm` trades a little idle-time parsing for an instant first open, and
    // is the right call only for overlays that are both small and reached by a
    // direct click, where a chunk fetch would read as lag. It is off by default:
    // warming an expensive overlay just moves the parse off first paint and
    // into the window where the operator is starting to interact, which on a
    // slow machine is the worst place for it.
    useEffect(() => {
      if (!warm || everOpened) return
      const idle = window.requestIdleCallback
      if (!idle) {
        const t = window.setTimeout(() => void load(), 2_000)
        return () => window.clearTimeout(t)
      }
      const handle = idle(() => void load(), { timeout: 5_000 })
      return () => window.cancelIdleCallback?.(handle)
    }, [everOpened])

    if (!everOpened) return null

    // fallback={null} on purpose: these are overlays, and the flag that opened
    // them is already true, so there is nothing on screen yet to replace. A
    // spinner here would flash a box before the dialog's own entry animation.
    return (
      <Suspense fallback={null}>
        <Overlay />
      </Suspense>
    )
  }
}

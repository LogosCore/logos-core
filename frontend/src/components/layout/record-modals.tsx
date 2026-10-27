import { lazy, Suspense, useState } from "react"
import { useSearchParams } from "react-router"
import { recordModalKey, recordModalOf } from "@/lib/record-modals"

// Lazy: the modals and everything they show load the first time one opens,
// not with the shell every page waits for.
const BuildModal = lazy(() => import("@/components/builds/build-modal").then((m) => ({ default: m.BuildModal })))
const EndpointModal = lazy(() =>
  import("@/components/endpoints/endpoint-modal").then((m) => ({ default: m.EndpointModal })),
)

/**
 * The build or endpoint modal the URL names (lib/record-modals.ts), over
 * whatever page is showing, so any page can open one and a link to one
 * works anywhere.
 */
export function RecordModals() {
  const [params] = useSearchParams()
  const modal = recordModalOf(params)
  // The last modal the URL named. Once the URL drops it, it stays on screen
  // closed while it fades out, and goes when the fade ends.
  const [shown, setShown] = useState(modal)
  if (modal && recordModalKey(modal) !== recordModalKey(shown)) setShown(modal)
  const current = modal ?? shown
  if (!current) return null
  const open = modal !== null
  const closed = () => setShown(null)
  return (
    <Suspense fallback={null}>
      {current.kind === "build" || current.kind === "new-build" ? (
        <BuildModal modal={current} open={open} onClosed={closed} />
      ) : (
        <EndpointModal modal={current} open={open} onClosed={closed} />
      )}
    </Suspense>
  )
}

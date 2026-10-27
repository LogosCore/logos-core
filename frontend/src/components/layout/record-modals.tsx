import { lazy, Suspense } from "react"
import { useSearchParams } from "react-router"
import { recordModalOf } from "@/lib/record-modals"

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
  if (!modal) return null
  return (
    <Suspense fallback={null}>
      {modal.kind === "build" || modal.kind === "new-build" ? (
        <BuildModal modal={modal} />
      ) : (
        <EndpointModal modal={modal} />
      )}
    </Suspense>
  )
}

import { Suspense, useEffect } from "react"
import { useSearchParams } from "react-router"
import { useLoaded, whenIdle } from "@/lib/preloadable"
import { recordModalOf, type RecordModal } from "@/lib/record-modals"
import { buildModalCode, endpointModalCode, preloadRecordModals } from "./record-modal-code"

/**
 * The build or endpoint modal the URL names (lib/record-modals.ts), over
 * whatever page is showing, so any page can open one and a link to one
 * works anywhere.
 */
export function RecordModals() {
  const [params] = useSearchParams()
  const modal = recordModalOf(params)
  // Their code is not in the shell. Fetch it once the page has settled, so
  // the first modal opened shows at once (record-modal-code.ts).
  useEffect(() => whenIdle(preloadRecordModals), [])
  if (!modal) return null
  return (
    // Nothing in the modals suspends today. Should something, this keeps it
    // from reaching the page's boundary, which would blank the page.
    <Suspense fallback={null}>
      {modal.kind === "build" || modal.kind === "new-build" ? (
        <BuildModalLoader modal={modal} />
      ) : (
        <EndpointModalLoader modal={modal} />
      )}
    </Suspense>
  )
}

function BuildModalLoader({ modal }: { modal: Extract<RecordModal, { kind: "build" | "new-build" }> }) {
  const code = useLoaded(buildModalCode)
  return code && <code.BuildModal modal={modal} />
}

function EndpointModalLoader({ modal }: { modal: Extract<RecordModal, { kind: "endpoint" | "new-endpoint" }> }) {
  const code = useLoaded(endpointModalCode)
  return code && <code.EndpointModal modal={modal} />
}

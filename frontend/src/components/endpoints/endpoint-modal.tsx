import { RecordModal } from "@/components/ui/record-modal"
import { useRecordModals } from "@/hooks/use-record-modals"
import type { RecordModal as RecordModalTarget } from "@/lib/record-modals"
import { EditEndpoint, NewEndpoint } from "./endpoint-editor"

/**
 * The endpoint modal (components/layout/record-modals.tsx): an endpoint's
 * editor, or the form for a new one. Keyed by what it shows, so moving from
 * one to another starts that one afresh.
 */
export function EndpointModal({ modal }: { modal: Extract<RecordModalTarget, { kind: "endpoint" | "new-endpoint" }> }) {
  const { close } = useRecordModals()
  return (
    <RecordModal onClose={close}>
      {modal.kind === "endpoint" ? (
        <EditEndpoint key={`endpoint:${modal.id}`} id={modal.id} />
      ) : (
        <NewEndpoint key={`new:${modal.channel}:${modal.from ?? ""}`} channel={modal.channel} from={modal.from} />
      )}
    </RecordModal>
  )
}

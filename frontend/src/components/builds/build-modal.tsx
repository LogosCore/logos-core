import { RecordModal } from "@/components/ui/record-modal"
import { useRecordModals } from "@/hooks/use-record-modals"
import type { RecordModal as RecordModalTarget } from "@/lib/record-modals"
import { BuildDetails } from "./build-details"
import { BuildEditor } from "./build-editor"

/**
 * The build modal (components/layout/record-modals.tsx): a build's details,
 * or the form for a new one. Keyed by what it shows, so moving from one to
 * the other (Build again, a build started) starts that one afresh.
 */
export function BuildModal({ modal }: { modal: Extract<RecordModalTarget, { kind: "build" | "new-build" }> }) {
  const { close } = useRecordModals()
  return (
    <RecordModal onClose={close}>
      {modal.kind === "build" ? (
        <BuildDetails key={`build:${modal.id}`} id={modal.id} />
      ) : (
        <BuildEditor key={`new:${modal.factory}:${modal.from ?? ""}`} factory={modal.factory} from={modal.from} />
      )}
    </RecordModal>
  )
}

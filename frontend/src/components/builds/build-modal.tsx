import { RecordModal } from "@/components/ui/record-modal"
import { buildEditorCode } from "@/components/layout/record-modal-code"
import { useRecordModals } from "@/hooks/use-record-modals"
import { useLoaded } from "@/lib/preloadable"
import type { RecordModal as RecordModalTarget } from "@/lib/record-modals"
import { BuildDetails } from "./build-details"

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
        <NewBuild key={`new:${modal.factory}:${modal.from ?? ""}`} factory={modal.factory} from={modal.from} />
      )}
    </RecordModal>
  )
}

/** The form for a new build, whose code is a chunk of its own (record-modal-code.ts). */
function NewBuild({ factory, from }: { factory: string; from: string | null }) {
  const code = useLoaded(buildEditorCode)
  return code && <code.BuildEditor factory={factory} from={from} />
}

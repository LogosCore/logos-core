import { create } from "zustand"
import type { Editor } from "@tiptap/core"
import { CredentialPickerDialog } from "@/components/findings/credential-picker-dialog"

/**
 * Singleton picker driven by a tiny Zustand store. The slash command opens it
 * with the active editor + operation, the dialog renders inside the React tree
 * (so the GraphQL client / React Query providers are in scope), and on pick it
 * inserts a `wikiCredentialReference` node at the captured insertion position.
 *
 * The list/create UI lives in the shared {@link CredentialPickerDialog} — this
 * file only owns the editor-insertion concern: where the reference lands and
 * how it's encoded. The editor is kept off React's render-driven state — the
 * insertion reads it via `getState()` so we don't churn re-renders for a
 * transient reference.
 */
/** Either inserts a reference into an editor, or hands the picked id to
 * `onPick` (a drawing places a card with it). */
type PickerArgs =
  | { editor: Editor; operationId: string; insertPos: number; onPick?: undefined }
  | { operationId: string; onPick: (id: string) => void; editor?: undefined; insertPos?: undefined }

interface PickerState {
  open: boolean
  editor: Editor | null
  operationId: string
  insertPos: number | null
  onPick: ((id: string) => void) | null
  openPicker: (args: PickerArgs) => void
  closePicker: () => void
}

const useWikiCredentialPickerStore = create<PickerState>((set) => ({
  open: false,
  editor: null,
  operationId: "",
  insertPos: null,
  onPick: null,
  openPicker: ({ editor, operationId, insertPos, onPick }) =>
    set({ open: true, editor: editor ?? null, operationId, insertPos: insertPos ?? null, onPick: onPick ?? null }),
  closePicker: () =>
    set({ open: false, editor: null, operationId: "", insertPos: null, onPick: null }),
}))

/** Imperative entry point — called from the slash-command item. Co-located
 *  with the dialog because both share the singleton store; splitting would
 *  require a third file just to host that. */
// eslint-disable-next-line react-refresh/only-export-components
export function openCredentialPicker(args: PickerArgs) {
  useWikiCredentialPickerStore.getState().openPicker(args)
}

export function WikiCredentialPickerDialog() {
  const open = useWikiCredentialPickerStore((s) => s.open)
  const operationId = useWikiCredentialPickerStore((s) => s.operationId)
  const closePicker = useWikiCredentialPickerStore((s) => s.closePicker)
  const forDrawing = useWikiCredentialPickerStore((s) => s.onPick !== null)

  // Shared insertion path — both the list pick and the create-then-insert flow
  // funnel through here so the editor side stays in one place. Reads the editor
  // imperatively so it never participates in render.
  function insertCredential(credentialId: string) {
    const { editor, insertPos, onPick } = useWikiCredentialPickerStore.getState()
    if (onPick) {
      closePicker()
      onPick(credentialId)
      return
    }
    if (!editor) {
      closePicker()
      return
    }
    const pos = insertPos ?? editor.state.selection.from
    editor
      .chain()
      .focus()
      .insertContentAt(pos, {
        type: "wikiCredentialReference",
        attrs: { credentialId },
      })
      .run()
    closePicker()
  }

  return (
    <CredentialPickerDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) closePicker()
      }}
      operationId={operationId}
      title={forDrawing ? "Add credential to drawing" : "Insert credential reference"}
      description={
        forDrawing
          ? "Pick a credential from this operation to draw as a card."
          : "Pick a credential from this operation to embed in the document."
      }
      createDescription="Add a credential to this operation and insert a reference to it."
      createSubmitLabel="Create & insert"
      createIdPrefix="wiki-cred-create"
      onPick={(c) => insertCredential(c.id)}
    />
  )
}

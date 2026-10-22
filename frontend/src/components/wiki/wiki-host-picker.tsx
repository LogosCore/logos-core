import { useState } from "react"
import { create } from "zustand"
import type { Editor } from "@tiptap/core"
import { ServerIcon } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { HostPickerList } from "@/components/findings/host-picker-list"

/**
 * Singleton picker driven by a tiny Zustand store. The slash command opens it
 * with the active editor + operation; on pick it inserts a `wikiHostReference`
 * node at the captured insertion position. Sibling of the hash / credential
 * pickers — list-only because hosts are created/imported on the Findings tab
 * rather than hand-built one at a time from the editor.
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

const useWikiHostPickerStore = create<PickerState>((set) => ({
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
 *  with the dialog because both share the singleton store. */
// eslint-disable-next-line react-refresh/only-export-components
export function openHostPicker(args: PickerArgs) {
  useWikiHostPickerStore.getState().openPicker(args)
}

export function WikiHostPickerDialog() {
  const open = useWikiHostPickerStore((s) => s.open)
  const closePicker = useWikiHostPickerStore((s) => s.closePicker)
  const forDrawing = useWikiHostPickerStore((s) => s.onPick !== null)
  const [search, setSearch] = useState("")

  // Reset the search field when the picker reopens.
  const [wasOpen, setWasOpen] = useState(open)
  if (wasOpen !== open) {
    setWasOpen(open)
    if (open) setSearch("")
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) closePicker()
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ServerIcon className="size-4" />
            {forDrawing ? "Add host to drawing" : "Insert host reference"}
          </DialogTitle>
          <DialogDescription>
            {forDrawing
              ? "Pick a host from this operation to draw as a card."
              : "Pick a host from this operation to embed in the document."}
          </DialogDescription>
        </DialogHeader>
        {open ? <PickerBody search={search} setSearch={setSearch} /> : null}
      </DialogContent>
    </Dialog>
  )
}

interface PickerBodyProps {
  search: string
  setSearch: (s: string) => void
}

function PickerBody({ search, setSearch }: PickerBodyProps) {
  const operationId = useWikiHostPickerStore((s) => s.operationId)
  const closePicker = useWikiHostPickerStore((s) => s.closePicker)

  function insertHost(hostId: string) {
    const { editor, insertPos, onPick } = useWikiHostPickerStore.getState()
    if (onPick) {
      closePicker()
      onPick(hostId)
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
        type: "wikiHostReference",
        attrs: { hostId },
      })
      .run()
    closePicker()
  }

  return (
    <HostPickerList
      operationId={operationId}
      search={search}
      onSearchChange={setSearch}
      onPick={insertHost}
    />
  )
}

import { useState } from "react"
import { FolderIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { errorMessage } from "@/components/modules/management/api-errors"
import { DocumentIconPicker } from "@/components/wiki/document-icon-picker"
import type { MinionSessionFolderFieldsFragment } from "@/graphql/gql/graphql"
import { useMinionSessionFolderMutations } from "@/graphql/hooks/minion-sessions"

// A folder of sessions made or changed: its name, and its icon (or emoji)
// with the icon's color, as a wiki page's (the same picker).

/** What the dialog is for: a new folder, or one to change (and maybe delete). */
export type FolderDialogTarget = { kind: "new" } | { kind: "edit"; folder: MinionSessionFolderFieldsFragment } | { kind: "delete"; folder: MinionSessionFolderFieldsFragment }

export function SessionFolderDialog({
  operationId,
  target,
  onClose,
  onCreated,
}: {
  operationId: string
  target: FolderDialogTarget | null
  onClose: () => void
  /** A new folder was made: shown, say. */
  onCreated?: (folder: MinionSessionFolderFieldsFragment) => void
}) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        {target?.kind === "delete" ? (
          <DeleteFolder operationId={operationId} folder={target.folder} onClose={onClose} />
        ) : target ? (
          <FolderForm
            key={target.kind === "edit" ? target.folder.id : "new"}
            operationId={operationId}
            folder={target.kind === "edit" ? target.folder : null}
            onClose={onClose}
            onCreated={onCreated}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function FolderForm({
  operationId,
  folder,
  onClose,
  onCreated,
}: {
  operationId: string
  folder: MinionSessionFolderFieldsFragment | null
  onClose: () => void
  onCreated?: (folder: MinionSessionFolderFieldsFragment) => void
}) {
  const { create, update } = useMinionSessionFolderMutations(operationId)
  const [name, setName] = useState(folder?.name ?? "")
  const [look, setLook] = useState({ emoji: folder?.emoji ?? "", icon: folder?.icon ?? "", color: folder?.color ?? "" })
  const busy = create.isPending || update.isPending
  const save = async () => {
    const input = { name: name.trim(), icon: look.icon || null, emoji: look.emoji || null, color: look.color || null }
    try {
      if (folder) {
        await update.mutateAsync({ id: folder.id, input })
      } else {
        const made = await create.mutateAsync(input)
        onCreated?.(made.createMinionSessionFolder)
      }
      onClose()
    } catch (e) {
      toast.error(errorMessage(e, folder ? "Failed to change the folder" : "Failed to make the folder"))
    }
  }
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void save()
      }}
    >
      <DialogHeader>
        <DialogTitle>{folder ? "Edit folder" : "New folder"}</DialogTitle>
        <DialogDescription>{folder ? "Rename it, or change its icon." : "A folder of sessions, shared with the operation."}</DialogDescription>
      </DialogHeader>
      <div className="flex items-center gap-2 py-4">
        <DocumentIconPicker
          value={look}
          onSelect={setLook}
          allowAdaptive={false}
          derivedDefault={{ glyph: <FolderIcon className="size-[18px]" style={look.color ? { color: look.color } : undefined} />, label: "Folder" }}
        />
        <Input
          aria-label="Folder name"
          placeholder="Folder name"
          autoFocus
          value={name}
          maxLength={64}
          onChange={(e) => setName(e.target.value)}
          className="flex-1"
        />
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={busy || name.trim() === ""}>
          {folder ? "Save" : "Create"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function DeleteFolder({ operationId, folder, onClose }: { operationId: string; folder: MinionSessionFolderFieldsFragment; onClose: () => void }) {
  const { remove } = useMinionSessionFolderMutations(operationId)
  return (
    <>
      <DialogHeader>
        <DialogTitle>Delete the folder {folder.name}?</DialogTitle>
        <DialogDescription>Its sessions stay, in All: only the folder goes, for everyone in the operation.</DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="destructive"
          disabled={remove.isPending}
          onClick={async () => {
            try {
              await remove.mutateAsync(folder.id)
              onClose()
            } catch (e) {
              toast.error(errorMessage(e, "Failed to delete the folder"))
            }
          }}
        >
          Delete
        </Button>
      </DialogFooter>
    </>
  )
}

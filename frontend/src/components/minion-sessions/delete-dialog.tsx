import { useState } from "react"
import { Loader2Icon, Trash2Icon } from "lucide-react"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// A listing's Delete (docs: contracts/task-results-and-actions.md,
// delete_path): always confirmed, with the full path. A folder asks, too,
// whether everything in it goes (recursive); without that, one that is not
// empty stays, and the task says so.

/** What a Delete names: an entry of a listing. */
export interface DeleteTarget {
  path: string
  name: string
  /** The entry's type: dir, file, link or other. */
  type: string
}

const KIND: Record<string, string> = { dir: "folder", file: "file", link: "link", other: "entry" }

export function DeleteDialog({
  target,
  commandId,
  onOpenChange,
  onDelete,
}: {
  /** What to delete; null: closed. */
  target: DeleteTarget | null
  commandId: string | null
  onOpenChange: (open: boolean) => void
  /** Runs the command with the action's params. */
  onDelete: (args: { path: string; recursive: boolean }) => Promise<unknown>
}) {
  return (
    <Dialog open={target !== null && commandId !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {target && commandId && (
          <DeleteForm key={target.path} target={target} commandId={commandId} onClose={() => onOpenChange(false)} onDelete={onDelete} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function DeleteForm({
  target,
  commandId,
  onClose,
  onDelete,
}: {
  target: DeleteTarget
  commandId: string
  onClose: () => void
  onDelete: (args: { path: string; recursive: boolean }) => Promise<unknown>
}) {
  const [recursive, setRecursive] = useState(false)
  const [pending, setPending] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const kind = KIND[target.type] ?? "entry"
  const folder = target.type === "dir"

  const submit = async () => {
    setPending(true)
    setFailure(null)
    try {
      await onDelete({ path: target.path, recursive: folder && recursive })
      onClose()
    } catch (err) {
      setFailure(errorMessage(err, "Could not delete it"))
      setPending(false)
    }
  }

  return (
    <form
      className="flex min-w-0 flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>Delete this {kind}?</DialogTitle>
        <DialogDescription>
          The minion removes it from its host with <span className="font-mono">{commandId}</span>. This cannot be undone.
          {target.type === "link" && " The link goes, not what it points to."}
        </DialogDescription>
      </DialogHeader>
      <p className="rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs break-all">{target.path}</p>
      {folder && (
        <label className="flex items-start gap-2 text-sm">
          <Checkbox className="mt-0.5" checked={recursive} disabled={pending} onCheckedChange={(on) => setRecursive(on === true)} />
          <span>
            Delete everything in it
            <span className="block text-xs text-muted-foreground">Unticked, a folder that is not empty stays, and the task says so.</span>
          </span>
        </label>
      )}
      {failure && <p className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{failure}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant="destructive" disabled={pending}>
          {pending ? <Loader2Icon className="animate-spin" /> : <Trash2Icon />}
          Delete
        </Button>
      </DialogFooter>
    </form>
  )
}

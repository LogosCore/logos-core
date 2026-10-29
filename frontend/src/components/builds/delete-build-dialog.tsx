import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { errorMessage } from "@/components/modules/management/api-errors"
import { isUnfinished, useDeleteBuild } from "@/graphql/hooks/builds"

/** What the dialog needs of a build: the table's rows and the build page's record both have it. */
export interface DeletableBuild {
  id: string
  moduleName: string
  target: string
  state: string
}

/**
 * Asks before deleting a build, whatever its state. The build, its artifact
 * and its minion go; a running build is cancelled first. onDeleted runs after
 * the build is gone (the build page closes itself there).
 */
export function DeleteBuildDialog({
  build,
  onClose,
  onDeleted,
}: {
  build: DeletableBuild | null
  onClose: () => void
  onDeleted?: () => void
}) {
  const del = useDeleteBuild()
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    if (!build) return
    setError(null)
    try {
      await del.mutateAsync(build.id)
      toast.success("Build deleted")
      onClose()
      onDeleted?.()
    } catch (err) {
      setError(errorMessage(err, "Failed to delete the build"))
    }
  }

  return (
    <Dialog
      open={build !== null}
      onOpenChange={(o) => {
        if (!o && !del.isPending) {
          setError(null)
          onClose()
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            Delete the build of{" "}
            <span className="font-mono">
              {build?.moduleName} · {build?.target}
            </span>
            ?
          </DialogTitle>
          <DialogDescription>
            {build && isUnfinished(build.state) && "It is still running and is cancelled first. "}
            The build record, its artifact and its minion are removed for good. A minion of this build that is already
            out can no longer open a session.
          </DialogDescription>
        </DialogHeader>
        {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={del.isPending}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={del.isPending}>
            {del.isPending ? "Deleting..." : "Delete"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

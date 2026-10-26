import { useState } from "react"
import { BanIcon, ShieldCheckIcon } from "lucide-react"
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
import { Field, FieldLabel } from "@/components/ui/field"
import { Textarea } from "@/components/ui/textarea"
import { useModuleStore } from "@/stores/modules"
import { useBlockModule, useDeleteModule } from "@/graphql/hooks/modules"
import { errorMessage } from "./management/api-errors"
import { useUnblockWithToast } from "./use-unblock"

/**
 * Block (with the confirmation of ModuleAdminDialogs, rendered alongside) or
 * Unblock for one instance, where it is shown on its own page.
 */
export function ModuleBlockButton({ module: m }: { module: { instance: string; type: string; status: string } }) {
  const openDialog = useModuleStore((s) => s.openDialog)
  const { unblock, pending } = useUnblockWithToast()
  if (m.status === "blocked") {
    return (
      <Button variant="outline" size="sm" onClick={() => unblock(m.instance)} disabled={pending}>
        <ShieldCheckIcon />
        {pending ? "Unblocking..." : "Unblock"}
      </Button>
    )
  }
  return (
    <Button
      variant="outline"
      size="sm"
      className="text-destructive hover:text-destructive"
      onClick={() => openDialog({ kind: "block", module: { instance: m.instance, type: m.type } })}
    >
      <BanIcon />
      Block…
    </Button>
  )
}

/** The confirmations of the Modules page's Block and Delete. */
export function ModuleAdminDialogs() {
  const dialog = useModuleStore((s) => s.dialog)
  const closeDialog = useModuleStore((s) => s.closeDialog)
  return (
    <Dialog open={dialog !== null} onOpenChange={(open) => !open && closeDialog()}>
      <DialogContent className="sm:max-w-md">
        {/* Keyed so each opening starts empty. */}
        {dialog?.kind === "block" && <BlockBody key={dialog.module.instance} instance={dialog.module.instance} onDone={closeDialog} />}
        {dialog?.kind === "delete" && <DeleteBody key={dialog.module.instance} instance={dialog.module.instance} onDone={closeDialog} />}
      </DialogContent>
    </Dialog>
  )
}

function BlockBody({ instance, onDone }: { instance: string; onDone: () => void }) {
  const block = useBlockModule()
  const [reason, setReason] = useState("")
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    setError(null)
    try {
      await block.mutateAsync({ instance, reason: reason.trim() || null })
      toast.success(`Blocked ${instance}`)
      onDone()
    } catch (err) {
      setError(errorMessage(err, "Failed to block the module"))
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Block {instance}?</DialogTitle>
        <DialogDescription>
          Core will refuse its registrations and traffic until you unblock it. If it is running, it is cut off now:
          a channel's clients get 404, a factory gets no builds. It keeps asking in the background, so it comes back by
          itself once unblocked.
        </DialogDescription>
      </DialogHeader>
      <Field>
        <FieldLabel htmlFor="block-reason">Reason (optional)</FieldLabel>
        <Textarea
          id="block-reason"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          maxLength={500}
          placeholder="Shown with the block"
        />
      </Field>
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        <Button variant="outline" onClick={onDone} disabled={block.isPending}>
          Cancel
        </Button>
        <Button variant="destructive" onClick={confirm} disabled={block.isPending}>
          {block.isPending ? "Blocking..." : "Block"}
        </Button>
      </DialogFooter>
    </>
  )
}

function DeleteBody({ instance, onDone }: { instance: string; onDone: () => void }) {
  const del = useDeleteModule()
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    setError(null)
    try {
      await del.mutateAsync({ instance })
      toast.success(`Deleted ${instance}`)
      onDone()
    } catch (err) {
      setError(errorMessage(err, "Failed to delete the module"))
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Delete {instance}?</DialogTitle>
        <DialogDescription>
          It disappears from the list. Its stored configuration and history are kept: if it registers again, it comes
          back with them.
        </DialogDescription>
      </DialogHeader>
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        <Button variant="outline" onClick={onDone} disabled={del.isPending}>
          Cancel
        </Button>
        <Button variant="destructive" onClick={confirm} disabled={del.isPending}>
          {del.isPending ? "Deleting..." : "Delete"}
        </Button>
      </DialogFooter>
    </>
  )
}

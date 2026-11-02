import { useState } from "react"
import { ListChecksIcon, Trash2Icon } from "lucide-react"
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
import { useDeleteBuilds } from "@/graphql/hooks/builds"

/** The builds a list shows: its search and filters, as the server takes them. */
export interface BuildsMatching {
  search: string | null
  states: string[] | null
  moduleNames: string[] | null
}

/**
 * The bar while builds are selected: how many, "select all matching" once
 * every loaded row is picked and more match, Delete (asked first: the builds,
 * their artifacts and their minions go for good, running ones cancelled),
 * and Done.
 */
export function DeleteBuildsBar({
  operationId,
  all,
  picked,
  loaded,
  total,
  matching,
  onSelectAll,
  onDone,
}: {
  operationId: string
  /** Every build the list matches is selected, loaded or not. */
  all: boolean
  picked: string[]
  loaded: number
  total: number
  matching: BuildsMatching
  onSelectAll: () => void
  onDone: () => void
}) {
  const del = useDeleteBuilds()
  const [confirming, setConfirming] = useState(false)
  const count = all ? total : picked.length
  const noun = count === 1 ? "build" : "builds"

  async function run() {
    try {
      const data = await del.mutateAsync({ operationId, ...(all ? { matching } : { ids: picked }) })
      const n = data.deleteBuilds
      toast.success(`Deleted ${n} ${n === 1 ? "build" : "builds"}`)
      setConfirming(false)
      onDone()
    } catch (err) {
      toast.error("Failed to delete the builds", { description: errorMessage(err, "Request failed") })
    }
  }

  return (
    <span className="inline-flex flex-wrap items-center gap-1.5 rounded-md border border-foreground/30 bg-muted px-2 py-0.5 text-xs">
      <ListChecksIcon className="size-3 shrink-0" />
      <span className="font-medium">{all ? `All ${total} matching selected` : `${count} selected`}</span>
      {!all && picked.length === loaded && total > loaded && (
        <Button variant="link" size="xs" className="h-auto px-0 text-xs" onClick={onSelectAll}>
          Select all {total} matching
        </Button>
      )}
      <Button size="xs" variant="destructive" disabled={count === 0 || del.isPending} onClick={() => setConfirming(true)}>
        <Trash2Icon />
        Delete
      </Button>
      <Button size="xs" variant="ghost" onClick={onDone} title="Done (Esc)">
        Done
      </Button>
      <Dialog open={confirming} onOpenChange={(o) => !o && !del.isPending && setConfirming(false)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              Delete {all ? `all ${total} matching` : count} {noun}?
            </DialogTitle>
            <DialogDescription>
              {all && "Every build the search and filters match goes, loaded or not. "}
              The build records, their artifacts and their minions are removed for good; running builds are cancelled
              first. A minion of these builds that is already out can no longer open a session.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirming(false)} disabled={del.isPending}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => void run()} disabled={del.isPending}>
              {del.isPending ? "Deleting..." : `Delete ${count} ${noun}`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </span>
  )
}

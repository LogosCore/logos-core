import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/**
 * Asked when leaving a record's JSON tab while its text does not read as the
 * record (what: "an endpoint", "a build"): the form can only show the last
 * version that did, so what changed since is dropped.
 */
export function LeaveJsonDialog({
  open,
  what,
  problem,
  onStay,
  onLeave,
}: {
  open: boolean
  what: string
  /** Why the text does not read, as the reader said. */
  problem: string
  onStay: () => void
  onLeave: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onStay()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>The JSON does not read as {what}</DialogTitle>
          <DialogDescription>
            {problem} The form shows the last version that did; what you changed since is dropped.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onStay}>
            Keep editing
          </Button>
          <Button type="button" onClick={onLeave}>
            Show the form
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

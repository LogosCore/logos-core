import { useState } from "react"
import { Loader2Icon, OctagonXIcon } from "lucide-react"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

// A process list's Kill (docs: contracts/task-results-and-actions.md,
// kill_process): always confirmed, naming the process. The minion ends it
// only if the process with its pid started when the list said (a pid reused
// since is not ended; with no start time, whatever has the pid); Force ends
// it at once, without letting it clean up. This session's minion's own
// process: the minion sends the task's done part, then exits.

/** What a Kill names: a process of a list. */
export interface KillTarget {
  pid: number
  name: string
  /** When it started, unix seconds, as the list said it; 0 when it did not say. */
  started: number
  /** Its executable or its command line, to tell it apart. */
  detail: string | null
  /** This session's minion's own process. */
  minion: boolean
}

export function KillDialog({
  target,
  commandId,
  onOpenChange,
  onKill,
}: {
  /** What to kill; null: closed. */
  target: KillTarget | null
  commandId: string | null
  onOpenChange: (open: boolean) => void
  /** Runs the command with the action's params. */
  onKill: (args: { pid: number; started: number; force: boolean }) => Promise<unknown>
}) {
  return (
    <Dialog open={target !== null && commandId !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {target && commandId && (
          <KillForm key={target.pid} target={target} commandId={commandId} onClose={() => onOpenChange(false)} onKill={onKill} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function KillForm({
  target,
  commandId,
  onClose,
  onKill,
}: {
  target: KillTarget
  commandId: string
  onClose: () => void
  onKill: (args: { pid: number; started: number; force: boolean }) => Promise<unknown>
}) {
  const [force, setForce] = useState(false)
  const [pending, setPending] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)

  const submit = async () => {
    setPending(true)
    setFailure(null)
    try {
      await onKill({ pid: target.pid, started: target.started, force })
      onClose()
    } catch (err) {
      setFailure(errorMessage(err, "Could not kill it"))
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
        <DialogTitle>Kill this process?</DialogTitle>
        <DialogDescription>
          The minion ends it on its host with <span className="font-mono">{commandId}</span>
          {target.started > 0
            ? ", if the process with its pid is still the one listed: one that took the pid since stays, and the task says so."
            : ": the list did not say when it started, so whatever process has its pid now ends."}
        </DialogDescription>
      </DialogHeader>
      <div className="rounded-md border bg-muted/40 px-3 py-2 text-sm">
        <div>
          {target.name} <span className="font-mono text-xs text-muted-foreground">pid {target.pid}</span>
        </div>
        {target.detail && <div className="mt-0.5 font-mono text-xs break-all text-muted-foreground">{target.detail}</div>}
      </div>
      {target.minion && (
        <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
          This is this session&apos;s minion. It says the task is done, then ends: it takes no more tasks.
        </p>
      )}
      <label className="flex items-start gap-2 text-sm">
        <Checkbox className="mt-0.5" checked={force} disabled={pending} onCheckedChange={(on) => setForce(on === true)} />
        <span>
          Force
          <span className="block text-xs text-muted-foreground">
            End it at once, without letting it clean up (SIGKILL). Unticked, it is asked to end (SIGTERM): it may clean up first, or not
            end at all. On Windows a minion may end it at once either way.
          </span>
        </span>
      </label>
      {failure && <p className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{failure}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant="destructive" disabled={pending}>
          {pending ? <Loader2Icon className="animate-spin" /> : <OctagonXIcon />}
          Kill
        </Button>
      </DialogFooter>
    </form>
  )
}

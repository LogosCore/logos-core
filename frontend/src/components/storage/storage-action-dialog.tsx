import { toast } from "sonner"
import { LoaderIcon, SquareIcon } from "lucide-react"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import { Skeleton } from "@/components/ui/skeleton"
import type { StorageActionQuery } from "@/graphql/gql/graphql"
import { useCancelStorageAction, useStorageAction } from "@/graphql/hooks/storage"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { ReclaimVolumesTable } from "./storage-tables"
import { actionResult, actionTitle, cancellable, reclaimCounts, recordingsText } from "./storage-values"

const STATUS: Record<string, string> = {
  running: "running",
  succeeded: "succeeded",
  failed: "failed",
  cancelled: "cancelled",
}

/**
 * One action's details, read again every 2 seconds while it runs: how it
 * went, what it does now, who asked it to stop; a reclaim's volumes, each as
 * it goes; Stop, for a reclaim or a cleanup that runs.
 */
export function StorageActionDialog({
  id,
  onOpenChange,
  canManage,
}: {
  id: string | null
  onOpenChange: (open: boolean) => void
  canManage: boolean
}) {
  const q = useStorageAction(id)
  const a = q.data?.storageAction
  const cancel = useCancelStorageAction()
  const stop = () => {
    if (!a) return
    const what = a.action === "reclaim" ? "the reclaim stops before its next volume" : "the cleanup stops before its next recording"
    void cancel.mutateAsync(a.id).then(
      () => toast.success(`Stopping: ${what}`),
      (err: unknown) => toast.error(errorMessage(err, "Could not stop it")),
    )
  }

  return (
    <Dialog open={id !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_auto_minmax(0,1fr)_auto] sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{a ? actionTitle(a) : "Action"}</DialogTitle>
          <DialogDescription>
            {q.error ? q.error.message : a ? actionResult(a) : q.data ? "There is no such action." : "Reading it…"}
          </DialogDescription>
        </DialogHeader>
        {!a ? (
          !q.data && !q.error && <Skeleton className="h-40 w-full" />
        ) : (
          <>
            <ActionCard action={a} />
            {a.action === "reclaim" && (
              <div className="flex h-72 min-h-0 flex-none flex-col">
                <ReclaimVolumesTable volumes={a.reclaimVolumes} />
              </div>
            )}
          </>
        )}
        <DialogFooter>
          {a && canManage && cancellable(a) && (
            <Button
              variant="destructive"
              onClick={stop}
              disabled={cancel.isPending}
              title={a.action === "reclaim" ? "It stops before its next volume" : "It stops before its next recording"}
            >
              {cancel.isPending ? <LoaderIcon className="animate-spin" /> : <SquareIcon />}
              Stop
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type Action = NonNullable<StorageActionQuery["storageAction"]>

/** How an action went: its status and step, who ran it, when, who asked it to stop, what it did. */
function ActionCard({ action: a }: { action: Action }) {
  const c = reclaimCounts(a.reclaimVolumes)
  return (
    <InfoCard labelWidth="w-36">
      <LabelRow label="Status">
        {STATUS[a.status] ?? a.status}
        {a.step && (
          <span className="block text-xs text-muted-foreground">
            {a.step}
            {a.stepAt && <span className="tabular-nums">, since {formatDateTime(a.stepAt)}</span>}
          </span>
        )}
        {a.error && <span className="block text-xs text-red-700 dark:text-red-300">{a.error}</span>}
      </LabelRow>
      <LabelRow label="By">{a.actor.name ?? a.actor.id}</LabelRow>
      <LabelRow label="Started">
        <span className="tabular-nums">{formatDateTime(a.startedAt)}</span>
        {a.finishedAt && <span className="tabular-nums text-muted-foreground"> · ended {formatDateTime(a.finishedAt)}</span>}
      </LabelRow>
      {a.cancelRequestedAt && (
        <LabelRow label="Asked to stop">
          {a.cancelledBy?.name ?? a.cancelledBy?.id ?? "an admin"}
          <span className="tabular-nums text-muted-foreground">, {formatDateTime(a.cancelRequestedAt)}</span>
        </LabelRow>
      )}
      {a.action === "reclaim" && (
        <>
          <LabelRow label="Freed">
            <span className="tabular-nums">{formatBytes(a.freed)}</span>
            {c.total > 0 && (
              <span className="block text-xs text-muted-foreground tabular-nums">
                {c.compacted} compacted, {c.skipped} skipped, {c.failed} failed, {c.stopped} stopped, of {c.total}
              </span>
            )}
          </LabelRow>
          {a.vacuumPaused && (
            <LabelRow label="SeaweedFS's vacuum">
              <span className="text-amber-700 dark:text-amber-300">paused while this runs</span>
            </LabelRow>
          )}
        </>
      )}
      {a.action === "cleanup" && (
        <LabelRow label="Took">
          <span className="tabular-nums">
            {recordingsText(a.recordings)}, {formatBytes(a.bytes)}
          </span>
          {a.plannedRecordings > 0 && (
            <span className="block text-xs text-muted-foreground tabular-nums">
              of about {recordingsText(a.plannedRecordings)}, {formatBytes(a.plannedBytes)}, found as it began
            </span>
          )}
        </LabelRow>
      )}
    </InfoCard>
  )
}

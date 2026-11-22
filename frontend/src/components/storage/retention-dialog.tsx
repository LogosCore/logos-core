import { useState } from "react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import type { StorageRetentionFieldsFragment } from "@/graphql/gql/graphql"
import { useSetStorageRetention, useStorageCleanupPreview } from "@/graphql/hooks/storage"
import { useDebounced } from "@/hooks/use-debounced"
import { formatBytes } from "@/lib/format-bytes"
import { MAX_RETENTION_DAYS, parseDays, recordingsText } from "./storage-values"

/**
 * Sets the retention policy (docs: Storage, Retention cleanups): on or off,
 * at an age in days. Turning it on says what its first cleanup takes at
 * that age: it starts at once.
 */
export function RetentionDialog({
  retention,
  open,
  onOpenChange,
}: {
  retention: StorageRetentionFieldsFragment
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>{open && <RetentionForm retention={retention} onDone={() => onOpenChange(false)} />}</DialogContent>
    </Dialog>
  )
}

/** The dialog's form, from the policy as it is when the dialog opens. */
function RetentionForm({ retention: r, onDone }: { retention: StorageRetentionFieldsFragment; onDone: () => void }) {
  const [enabled, setEnabled] = useState(r.enabled)
  const [days, setDays] = useState(String(r.olderThanDays))
  const age = parseDays(days)
  const turningOn = enabled && !r.enabled
  const shown = useDebounced(age, 400)
  const preview = useStorageCleanupPreview(turningOn ? shown : null)
  const first = preview.data?.pages[0]?.storageCleanupPreview
  const set = useSetStorageRetention()

  const save = () => {
    if (age === null) return
    void set.mutateAsync({ enabled, olderThanDays: age }).then(
      () => {
        toast.success(
          !enabled
            ? "The retention policy is off"
            : turningOn
              ? "The retention policy is on: its first cleanup starts now. Activity keeps each one."
              : "The retention policy is saved",
        )
        onDone()
      },
      (err: unknown) => toast.error(errorMessage(err, "Could not set the retention policy")),
    )
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Retention policy</DialogTitle>
        <DialogDescription>
          One for every operation. On, core cleans up each day the recordings that ended at least the age ago: each loses its files and
          keeps its record, marked cleaned up. Those of tasks marked important are kept.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4 text-sm">
        <label className="flex items-center gap-3">
          <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="Clean up each day" />
          Clean up each day
        </label>
        <label className="flex flex-wrap items-baseline gap-2">
          Recordings that ended at least
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            max={MAX_RETENTION_DAYS}
            value={days}
            onChange={(e) => setDays(e.target.value)}
            aria-label="Age in days"
            aria-invalid={age === null}
            className="h-7 w-24 self-center tabular-nums"
          />
          days ago
        </label>
        {age === null && (
          <p className="text-xs text-red-700 dark:text-red-300">The age is a whole number of days, 1 to {MAX_RETENTION_DAYS.toLocaleString("en-US")}.</p>
        )}
        {turningOn && age !== null && (
          <p className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-amber-700 dark:text-amber-300">
            Its first cleanup starts now.{" "}
            {!first || shown !== age
              ? "Reading what it takes…"
              : first.recordings === 0
                ? "At this age it takes nothing yet."
                : `At this age it takes ${recordingsText(first.recordings)}, ${formatBytes(first.bytes)}, in ${first.operations.totalCount} ${first.operations.totalCount === 1 ? "operation" : "operations"}.`}{" "}
            Nothing is kept elsewhere.
          </p>
        )}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button onClick={save} disabled={age === null || set.isPending}>
          Save
        </Button>
      </DialogFooter>
    </>
  )
}

import type { ReactNode } from "react"
import { CircleAlertIcon, EraserIcon, InfoIcon, PencilIcon, PlusIcon, TriangleAlertIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import type { StorageRetentionFieldsFragment } from "@/graphql/gql/graphql"
import { useNow } from "@/hooks/use-now"
import { formatDateTime, formatTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import {
  actionResult,
  bucketAbout,
  bucketName,
  daysText,
  deletedShare,
  MAX_RETENTION_DAYS,
  nextRunText,
  recordingsText,
  volumesText,
  type StorageBucket,
  type StorageDisk,
  type StorageStatus,
  type StorageWarning,
} from "./storage-values"

// The Storage page's cards: SeaweedFS as its master says, each volume
// server's disk, each bucket with what core keeps there; and the retention
// cleanups' (docs: Storage, Retention cleanups): the policy, and a cleanup
// run now.

const LABELS = "w-36"

const LEVEL: Record<string, { icon: typeof InfoIcon; className: string }> = {
  error: { icon: CircleAlertIcon, className: "border-red-500/30 bg-red-500/5 text-red-700 dark:text-red-300" },
  warning: { icon: TriangleAlertIcon, className: "border-amber-500/30 bg-amber-500/5 text-amber-700 dark:text-amber-300" },
  info: { icon: InfoIcon, className: "border-border bg-muted/40 text-muted-foreground" },
}

/** The store's warnings, the worst first: each a line with its level's mark. */
export function StorageWarnings({ warnings }: { warnings: readonly StorageWarning[] }) {
  if (warnings.length === 0) return null
  return (
    <ul className="flex flex-col gap-1.5">
      {warnings.map((w, i) => {
        const level = LEVEL[w.level] ?? LEVEL.info
        const Icon = level.icon
        return (
          <li key={`${w.code}:${w.bucket ?? ""}:${i}`} className={cn("flex items-baseline gap-2 rounded-md border px-3 py-2 text-sm", level.className)}>
            <Icon className="size-4 shrink-0 self-center" aria-label={w.level} />
            <span>{w.message}</span>
          </li>
        )
      })}
    </ul>
  )
}

/** SeaweedFS as its master says: whether core reaches it, its version, its volume slots. */
export function SeaweedFSCard({ status, actions }: { status: StorageStatus; actions?: ReactNode }) {
  const sw = status.seaweedfs
  return (
    <InfoCard title="SeaweedFS" note={sw.version ?? undefined} actions={actions} labelWidth={LABELS}>
      {!sw.configured ? (
        <NoteRow>
          Core is not set to reach SeaweedFS&apos;s master (SEAWEEDFS_MASTER_URL): the buckets show what core&apos;s records say alone, and
          no maintenance runs.
        </NoteRow>
      ) : !sw.reachable ? (
        <LabelRow label="Master">
          <span className="text-red-700 dark:text-red-300">out of reach: {sw.error}</span>
        </LabelRow>
      ) : (
        <>
          <LabelRow label="Master">{sw.leader ?? "answers"}</LabelRow>
          <LabelRow label="Volume slots">
            <span className="tabular-nums">
              {sw.freeSlots} free of {sw.slots}
            </span>
            <span className="block text-xs text-muted-foreground">
              About one a GB of free disk on the volume servers: a bucket that needs a new volume when none is free fails its writes.
            </span>
          </LabelRow>
        </>
      )}
      {status.recordsError && (
        <LabelRow label="Core's records">
          <span className="text-amber-700 dark:text-amber-300">unreadable: {status.recordsError}</span>
        </LabelRow>
      )}
      <LabelRow label="Read">
        <span className="tabular-nums" title={formatDateTime(status.at)}>
          {formatTime(status.at)}
        </span>
        <span className="text-xs text-muted-foreground"> · core reads the store once a minute</span>
      </LabelRow>
    </InfoCard>
  )
}

/** A volume server's disk: what it holds, and the store's part of it. */
export function DiskCard({ disk, store }: { disk: StorageDisk; store: number }) {
  if (disk.error) {
    return (
      <InfoCard title="Disk" note={disk.node} labelWidth={LABELS}>
        <NoteRow>Its disks do not read: {disk.error}</NoteRow>
      </InfoCard>
    )
  }
  const used = disk.total > 0 ? disk.used / disk.total : 0
  return (
    <InfoCard title="Disk" note={`${disk.node} ${disk.dir ?? ""}`.trim()} labelWidth={LABELS}>
      <LabelRow label="Used">
        <span className="tabular-nums">
          {formatBytes(disk.used)} of {formatBytes(disk.total)} ({Math.round(used * 100)}%)
        </span>
        <span className="mt-1 block h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden>
          <span className={cn("block h-full rounded-full", used >= 0.9 ? "bg-amber-500" : "bg-sky-500/70")} style={{ width: `${used * 100}%` }} />
        </span>
      </LabelRow>
      <LabelRow label="Free">
        <span className="tabular-nums">{formatBytes(disk.free)}</span>
      </LabelRow>
      <LabelRow label="The store's">
        <span className="tabular-nums">{formatBytes(store)}</span>
        <span className="block text-xs text-muted-foreground">Every bucket&apos;s volumes; the rest of what is used is not SeaweedFS&apos;s.</span>
      </LabelRow>
      <LabelRow label="Volumes">
        <span className="tabular-nums">
          {disk.volumes} of {disk.maxVolumes}
        </span>
      </LabelRow>
    </InfoCard>
  )
}

/**
 * A bucket: what core keeps there, by its records, and SeaweedFS's numbers
 * for it; its warnings; Add a volume, for one with none to write to.
 */
export function BucketCard({
  bucket: b,
  warnings,
  onGrow,
  growing,
}: {
  bucket: StorageBucket
  warnings: readonly StorageWarning[]
  /** Adds a writable volume; absent for a user who may not, and for SeaweedFS's own files. */
  onGrow?: () => void
  growing?: boolean
}) {
  const share = deletedShare(b)
  const mine = b.purposes.length > 0
  return (
    <InfoCard
      title={<span className="font-mono">{bucketName(b.name)}</span>}
      note={bucketAbout(b)}
      labelWidth={LABELS}
      actions={
        onGrow && (
          <Button variant="ghost" size="xs" onClick={onGrow} disabled={growing} title="Add a writable volume to this bucket: it takes one volume slot">
            <PlusIcon />
            Add a volume
          </Button>
        )
      }
    >
      {mine && (
        <LabelRow label="Core keeps">
          <span className="tabular-nums">
            {b.objects.toLocaleString("en-US")} {b.objects === 1 ? "object" : "objects"}, {formatBytes(b.objectBytes)}
          </span>
        </LabelRow>
      )}
      <LabelRow label="On disk">
        <span className="tabular-nums">{formatBytes(b.size)}</span>
        {b.deletedBytes > 0 && (
          <span className={cn("block text-xs", share >= 0.3 ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground")}>
            {formatBytes(b.deletedBytes)} of it deleted files ({Math.round(share * 100)}%), until reclaimed
          </span>
        )}
      </LabelRow>
      <LabelRow label="Volumes">{volumesText(b)}</LabelRow>
      {warnings.map((w, i) => (
        <NoteRow key={i}>
          <span className={w.level === "error" ? "text-red-700 dark:text-red-300" : w.level === "warning" ? "text-amber-700 dark:text-amber-300" : undefined}>
            {w.message}
          </span>
        </NoteRow>
      ))}
    </InfoCard>
  )
}

/**
 * The retention policy, one for every operation: on or off, its age, its
 * last cleanup and when the next runs; Edit, for an admin who may set it.
 */
export function RetentionCard({ retention: r, onEdit }: { retention: StorageRetentionFieldsFragment; onEdit?: () => void }) {
  const now = useNow(30_000)
  const last = r.lastRun
  return (
    <InfoCard
      title="Retention policy"
      note="every operation"
      labelWidth={LABELS}
      actions={
        onEdit && (
          <Button variant="ghost" size="xs" onClick={onEdit} title="Turn the policy on or off, and set its age">
            <PencilIcon />
            Edit
          </Button>
        )
      }
    >
      <LabelRow label="State">
        {r.enabled ? (
          <span className="text-emerald-700 dark:text-emerald-300">on: core cleans up each day</span>
        ) : (
          <span>off: core cleans up nothing on its own</span>
        )}
      </LabelRow>
      <LabelRow label="Age">
        <span className="tabular-nums">{daysText(r.olderThanDays)}</span>
        <span className="block text-xs text-muted-foreground">
          Recordings that ended earlier lose their files; those of tasks marked important keep them.
        </span>
      </LabelRow>
      <LabelRow label="Last cleanup">
        {last ? (
          <>
            <span className="tabular-nums">{formatDateTime(last.startedAt)}</span>
            <span className={cn("block text-xs", last.status === "failed" ? "text-red-700 dark:text-red-300" : "text-muted-foreground")}>
              {actionResult(last)}
            </span>
          </>
        ) : (
          <span className="text-muted-foreground">none yet</span>
        )}
      </LabelRow>
      {r.enabled && (
        <LabelRow label="Next cleanup">
          <span className="tabular-nums">{nextRunText(r.nextRunAt, now)}</span>
        </LabelRow>
      )}
      <LabelRow label="Set by">
        {r.updatedBy ? (
          <span>
            {r.updatedBy.name ?? r.updatedBy.id}
            {r.updatedAt && <span className="text-muted-foreground tabular-nums">, {formatDateTime(r.updatedAt)}</span>}
          </span>
        ) : (
          <span className="text-muted-foreground">no one yet</span>
        )}
      </LabelRow>
    </InfoCard>
  )
}

/** What a cleanup at an age would take now, in all: null while it is read. */
export interface CleanupTotals {
  recordings: number
  bytes: number
  operations: number
}

/**
 * A cleanup now: its age, what it would take at it, and the cleanup of every
 * operation's recordings (one operation's from the table below it).
 */
export function CleanupCard({
  days,
  onDays,
  ageError,
  totals,
  onCleanUp,
  busy,
}: {
  /** The age as typed, and its error: null for an age that reads. */
  days: string
  onDays: (v: string) => void
  ageError: string | null
  totals: CleanupTotals | null
  /** Cleans up every operation's; absent while there is nothing to take. */
  onCleanUp?: () => void
  /** A cleanup runs: none starts until it ended. */
  busy: boolean
}) {
  return (
    <InfoCard
      title="Clean up now"
      note="recordings alone"
      labelWidth={LABELS}
      actions={
        <Button
          variant="ghost"
          size="xs"
          onClick={onCleanUp}
          disabled={!onCleanUp || busy}
          title={busy ? "A cleanup runs: one at a time" : "Clean up the recordings of every operation that ended this long ago"}
        >
          <EraserIcon />
          Clean up every operation
        </Button>
      }
    >
      <LabelRow label="Ended at least">
        <span className="flex items-baseline gap-2">
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            max={MAX_RETENTION_DAYS}
            value={days}
            onChange={(e) => onDays(e.target.value)}
            aria-label="Age in days"
            aria-invalid={ageError !== null}
            className="h-7 w-24 self-center tabular-nums"
          />
          <span>days ago</span>
        </span>
        {ageError && <span className="block text-xs text-red-700 dark:text-red-300">{ageError}</span>}
      </LabelRow>
      <LabelRow label="It would take">
        {ageError ? (
          <span className="text-muted-foreground">—</span>
        ) : totals === null ? (
          <Skeleton className="h-4 w-48" />
        ) : totals.recordings === 0 ? (
          <span className="text-muted-foreground">nothing at this age</span>
        ) : (
          <span className="tabular-nums">
            {recordingsText(totals.recordings)}, {formatBytes(totals.bytes)}, in {totals.operations}{" "}
            {totals.operations === 1 ? "operation" : "operations"}
          </span>
        )}
      </LabelRow>
      <NoteRow>
        Each recording taken loses its files, and keeps its record, marked cleaned up: nothing is kept elsewhere. Those of tasks marked
        important, and those still recording, are kept. Reclaim space then gives the disk the bytes back.
      </NoteRow>
    </InfoCard>
  )
}

import type { ReactNode } from "react"
import { CircleAlertIcon, InfoIcon, PlusIcon, TriangleAlertIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { formatDateTime, formatTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import {
  bucketAbout,
  bucketName,
  deletedShare,
  volumesText,
  type StorageBucket,
  type StorageDisk,
  type StorageStatus,
  type StorageWarning,
} from "./storage-values"

// The Storage page's cards: SeaweedFS as its master says, each volume
// server's disk, and each bucket with what core keeps there.

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

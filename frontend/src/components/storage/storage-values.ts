import type { StorageActionFieldsFragment, StorageStatusQuery } from "@/graphql/gql/graphql"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"

// What the Storage page says of the store (core: storage.graphql): its
// buckets by what core keeps there, the kinds of records, the warnings' marks,
// the actions run, and the retention cleanups (docs: Storage, Retention
// cleanups).

export type StorageStatus = StorageStatusQuery["storageStatus"]
export type StorageBucket = StorageStatus["buckets"][number]
export type StorageDisk = StorageStatus["disks"][number]
export type StorageWarning = StorageStatus["warnings"][number]

const PURPOSES: Record<string, string> = {
  minion_files: "Minion files",
  recordings: "Recordings",
  builds: "Builds",
  wiki_images: "Wiki images",
  wiki_files: "Wiki files",
  skills: "Skills",
}

/** What core keeps in a bucket, in words: its purposes' names. */
export function purposeLabel(purpose: string): string {
  return PURPOSES[purpose] ?? purpose
}

/** A bucket's name as the page shows it: SeaweedFS's own files are the bucket named "". */
export function bucketName(name: string): string {
  return name === "" ? "SeaweedFS's own files" : name
}

/** What a bucket holds, in a few words: core's purposes, or whose it is. */
export function bucketAbout(b: Pick<StorageBucket, "name" | "purposes">): string {
  if (b.purposes.length > 0) return b.purposes.map(purposeLabel).join(", ")
  return b.name === "" ? "written outside any bucket" : "not core's"
}

const KINDS: Record<string, Record<string, string>> = {
  minion_files: { upload: "Task uploads", screenshot: "Screenshots", download: "Downloads" },
  recordings: { desktop: "Desktops", terminal: "Terminals" },
  builds: { file: "Build files", artifact: "Artifacts" },
  wiki_images: { image: "Images" },
  wiki_files: { file: "Files" },
  skills: { version: "Skill versions" },
}

/** A kind of record core keeps, in words: a minion file's, a recording's... */
export function kindLabel(purpose: string, kind: string): string {
  return KINDS[purpose]?.[kind] ?? kind
}

/** The share of a bucket's bytes on disk deleted files hold until reclaimed: 0 to 1. */
export function deletedShare(b: Pick<StorageBucket, "size" | "deletedBytes">): number {
  return b.size > 0 ? Math.min(b.deletedBytes / b.size, 1) : 0
}

/** The bucket the breakdown opens on: core's that holds the most by its records; null for none. */
export function defaultBucket(buckets: readonly Pick<StorageBucket, "name" | "purposes" | "objectBytes">[]): string | null {
  let best: (typeof buckets)[number] | null = null
  for (const b of buckets) {
    if (b.purposes.length > 0 && (best === null || b.objectBytes > best.objectBytes)) best = b
  }
  return best?.name ?? null
}

/** The store's bytes on disk: every bucket's volumes'. */
export function storeBytes(buckets: readonly Pick<StorageBucket, "size">[]): number {
  return buckets.reduce((n, b) => n + b.size, 0)
}

/** A bucket's volumes in words: how many, how many take new files, how many are full or read-only. */
export function volumesText(b: Pick<StorageBucket, "volumes" | "writableVolumes" | "readOnlyVolumes">): string {
  if (b.volumes === 0) return "none yet: SeaweedFS adds one at its first write"
  const parts = [`${b.writableVolumes} writable`]
  if (b.readOnlyVolumes > 0) parts.push(`${b.readOnlyVolumes} read-only`)
  return `${b.volumes} (${parts.join(", ")})`
}

export type WarningLevel = "error" | "warning" | "info"

/** The worst of the warnings' levels; null for none. */
export function worstLevel(warnings: readonly Pick<StorageWarning, "level">[]): WarningLevel | null {
  for (const level of ["error", "warning", "info"] as const) {
    if (warnings.some((w) => w.level === level)) return level
  }
  return null
}

/** The sidebar's mark: the warnings that need an admin (errors and warnings, not tips), and the worst of them. */
export function warningMark(warnings: readonly Pick<StorageWarning, "level">[]): { count: number; level: "error" | "warning" } | null {
  const needing = warnings.filter((w) => w.level === "error" || w.level === "warning")
  if (needing.length === 0) return null
  return { count: needing.length, level: needing.some((w) => w.level === "error") ? "error" : "warning" }
}

/** A bucket's warnings: those that name it. */
export function bucketWarnings(warnings: readonly StorageWarning[], name: string): StorageWarning[] {
  return warnings.filter((w) => w.bucket === name)
}

type ActionResult = Pick<StorageActionFieldsFragment, "action" | "status" | "error" | "freed" | "volumes" | "recordings" | "bytes">

/** What an action did, in words: its result, its error, or that it runs (a cleanup's: what it took so far). */
export function actionResult(a: ActionResult): string {
  if (a.action === "cleanup") return cleanupResult(a)
  switch (a.status) {
    case "running":
      return a.action === "reclaim" ? "running: SeaweedFS compacts the volumes" : "running"
    case "failed":
      return a.error ?? "failed"
  }
  return a.action === "reclaim" ? `freed ${formatBytes(a.freed)}` : `added ${a.volumes} ${a.volumes === 1 ? "volume" : "volumes"}`
}

/** A cleanup's result: the recordings it took and their bytes; so far while it runs; before it failed. */
function cleanupResult(a: ActionResult): string {
  const took = a.recordings > 0 ? `took ${recordingsText(a.recordings)}, ${formatBytes(a.bytes)}` : null
  switch (a.status) {
    case "running":
      return took ? `running: ${took} so far` : "running: none taken yet"
    case "failed":
      return `${a.error ?? "failed"}${took ? ` (it ${took} before)` : ""}`
  }
  return took ?? "took none"
}

/** How many recordings, in words. */
export function recordingsText(n: number): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? "recording" : "recordings"}`
}

/** An action in words: Reclaim space, Add a volume to a bucket, or a cleanup's age and the operations it covers. */
export function actionTitle(a: Pick<StorageActionFieldsFragment, "action" | "bucket" | "olderThanDays" | "operationId" | "operationName">): string {
  if (a.action === "cleanup") {
    const where = a.operationId === null ? "every operation" : (a.operationName ?? "an operation gone")
    return `Clean up recordings (${daysText(a.olderThanDays ?? 0)}, ${where})`
  }
  return a.action === "reclaim" ? "Reclaim space" : `Add a volume to ${bucketName(a.bucket ?? "")}`
}

/** The most days a cleanup's age may be (core: storage.MaxRetentionDays). */
export const MAX_RETENTION_DAYS = 3650

/** The age a cleanup is offered at, the policy's before anyone set it (core: models.DefaultRetentionDays). */
export const DEFAULT_RETENTION_DAYS = 30

/** The days an age input holds: a whole number, 1 to MAX_RETENTION_DAYS; null for anything else. */
export function parseDays(input: string): number | null {
  const t = input.trim()
  if (!/^\d+$/.test(t)) return null
  const n = Number(t)
  return n >= 1 && n <= MAX_RETENTION_DAYS ? n : null
}

/** An age in words. */
export function daysText(n: number): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? "day" : "days"}`
}

/**
 * When the policy's next cleanup runs, in words: its time, or that it is due
 * (core looks every 10 minutes, and at once when it is turned on); null while
 * the policy is off.
 */
export function nextRunText(nextRunAt: string | null | undefined, now: number): string | null {
  if (!nextRunAt) return null
  return Date.parse(nextRunAt) <= now ? "due: it starts within minutes" : formatDateTime(nextRunAt)
}

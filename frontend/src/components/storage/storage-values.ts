import type { StorageActionFieldsFragment, StorageStatusQuery } from "@/graphql/gql/graphql"
import { formatBytes } from "@/lib/format-bytes"

// What the Storage page says of the store (core: storage.graphql): its
// buckets by what core keeps there, the kinds of records, the warnings' marks.

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

/** What an action did, in words: its result, its error, or that it runs. */
export function actionResult(a: Pick<StorageActionFieldsFragment, "action" | "status" | "error" | "freed" | "volumes">): string {
  switch (a.status) {
    case "running":
      return a.action === "reclaim" ? "running: SeaweedFS compacts the volumes" : "running"
    case "failed":
      return a.error ?? "failed"
  }
  return a.action === "reclaim" ? `freed ${formatBytes(a.freed)}` : `added ${a.volumes} ${a.volumes === 1 ? "volume" : "volumes"}`
}

/** An action in words: Reclaim space, or Add a volume to a bucket. */
export function actionTitle(a: Pick<StorageActionFieldsFragment, "action" | "bucket">): string {
  return a.action === "reclaim" ? "Reclaim space" : `Add a volume to ${bucketName(a.bucket ?? "")}`
}

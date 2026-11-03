// A volumes task's data (docs: contracts/task-results-and-actions.md): the
// host's volumes, its drives and the file systems it has mounted, joined
// from the task's parts in order.

export interface Volume {
  /** Where its files are, as the host writes it: C:\, /boot. */
  path: string
  /** fixed, removable, optical, network, ram or other. */
  kind: VolumeKind
  /** What it is mounted from: /dev/sda1, \\server\share. */
  device?: string
  label?: string
  fs?: string
  /** Bytes; none where the host does not say. */
  size?: number
  /** Bytes the minion's user may still write. */
  free?: number
  readOnly: boolean
  /** A drive with nothing in it (an empty DVD drive or card reader). */
  noMedia: boolean
}

export type VolumeKind = "fixed" | "removable" | "optical" | "network" | "ram" | "other"

export interface Volumes {
  sep: "/" | "\\"
  volumes: Volume[]
}

const KINDS: readonly string[] = ["fixed", "removable", "optical", "network", "ram", "other"]

/** A kind as a volume says it: one a later version adds is "other". */
export function volumeKind(kind: unknown): VolumeKind {
  return typeof kind === "string" && KINDS.includes(kind) ? (kind as VolumeKind) : "other"
}

export const KIND_LABELS: Record<VolumeKind, string> = {
  fixed: "Fixed",
  removable: "Removable",
  optical: "Optical",
  network: "Network",
  ram: "RAM",
  other: "Other",
}

const text = (v: unknown) => (typeof v === "string" && v !== "" ? v : undefined)
const bytes = (v: unknown) => (typeof v === "number" && v >= 0 ? v : undefined)

/** The data as volumes, or null when it is not (shown raw then). */
export function readVolumes(data: unknown): Volumes | null {
  if (typeof data !== "object" || data === null) return null
  const d = data as Record<string, unknown>
  if (d.sep !== "/" && d.sep !== "\\") return null
  const volumes: Volume[] = []
  for (const v of Array.isArray(d.volumes) ? d.volumes : []) {
    if (typeof v !== "object" || v === null) return null
    const o = v as Record<string, unknown>
    if (typeof o.path !== "string" || o.path === "") return null
    volumes.push({
      path: o.path,
      kind: volumeKind(o.kind),
      device: text(o.device),
      label: text(o.label),
      fs: text(o.fs),
      size: bytes(o.size),
      free: bytes(o.free),
      readOnly: o.read_only === true,
      noMedia: o.no_media === true,
    })
  }
  return { sep: d.sep, volumes }
}

/** How full a volume is, 0 to 1; null when its size or free is not known. */
export function usedShare(v: Pick<Volume, "size" | "free">): number | null {
  if (v.size === undefined || v.free === undefined || v.size <= 0) return null
  return Math.min(Math.max((v.size - v.free) / v.size, 0), 1)
}

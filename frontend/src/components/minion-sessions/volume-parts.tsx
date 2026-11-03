import { DiscIcon, HardDriveIcon, LayersIcon, MemoryStickIcon, NetworkIcon, UsbIcon, type LucideIcon } from "lucide-react"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { KIND_LABELS, usedShare, type Volume, type VolumeKind } from "./volumes"

// A volume's parts as the volumes view and the file browser's roots show
// them: its kind's icon, and how full it is.

const KIND_ICONS: Record<VolumeKind, LucideIcon> = {
  fixed: HardDriveIcon,
  removable: UsbIcon,
  optical: DiscIcon,
  network: NetworkIcon,
  ram: MemoryStickIcon,
  other: LayersIcon,
}

export function VolumeKindIcon({ kind, className }: { kind: VolumeKind; className?: string }) {
  const Icon = KIND_ICONS[kind]
  return <Icon className={cn("size-3.5 shrink-0", className)} aria-label={KIND_LABELS[kind]} />
}

/** How full a volume is: a bar, and what is free of its size. */
export function VolumeUsage({ volume: v, className }: { volume: Pick<Volume, "size" | "free" | "noMedia">; className?: string }) {
  if (v.noMedia) return <span className={cn("text-muted-foreground", className)}>No media</span>
  const used = usedShare(v)
  if (used === null) {
    return <span className={cn("text-muted-foreground tabular-nums", className)}>{v.size !== undefined ? formatBytes(v.size) : ""}</span>
  }
  return (
    <span className={cn("inline-flex items-center gap-2", className)}>
      <span
        className="h-1.5 w-16 shrink-0 overflow-hidden rounded-full bg-muted"
        role="meter"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(used * 100)}
        aria-label="Used"
      >
        <span
          className={cn("block h-full rounded-full", used >= 0.9 ? "bg-red-500" : "bg-sky-500")}
          style={{ width: `${Math.max(used * 100, 2)}%` }}
        />
      </span>
      <span className="whitespace-nowrap text-muted-foreground tabular-nums">
        {formatBytes(v.free!)} free of {formatBytes(v.size!)}
      </span>
    </span>
  )
}

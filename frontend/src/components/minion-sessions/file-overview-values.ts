import { formatBytes } from "@/lib/format-bytes"

/** Bytes a second, and the time left at it, for a progress line. */
export function rateText(bps: number | null, left: number): string {
  if (bps === null || bps <= 0) return ""
  const secs = left / bps
  const time =
    secs < 60
      ? `${Math.max(1, Math.round(secs))}s`
      : secs < 3600
        ? `${Math.floor(secs / 60)}m ${Math.round(secs % 60)}s`
        : `${Math.floor(secs / 3600)}h ${Math.round((secs % 3600) / 60)}m`
  return ` · ${formatBytes(bps)}/s · ${time} left`
}

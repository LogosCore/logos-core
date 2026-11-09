import { formatAbsolute } from "@/lib/relative-time"

// A session's path through delegates (docs: contracts/minion-messages/
// delegation): the sessions whose delegate tasks carried its latest
// message, nearest first, as core recorded it with the message. Empty: a
// channel's instance brought it.

/** A delegate on the path: its session's number. */
export interface ViaHop {
  number: number
}

/** The path's short form, for a cell or a tab: "via #3", "via #3 +1" with more delegates behind it; "" for none. */
export function viaShort(via: readonly ViaHop[]): string {
  if (via.length === 0) return ""
  return via.length > 1 ? `via #${via[0].number} +${via.length - 1}` : `via #${via[0].number}`
}

/** The path for a hover: "Through session #3's delegate task, then #2's, since 2026.11.09 10:41:02"; "" for none. */
export function viaNote(via: readonly ViaHop[], since?: string | null): string {
  if (via.length === 0) return ""
  const behind = via.slice(1).map((v) => `#${v.number}'s`)
  const then = behind.length > 0 ? `, then ${behind.join(", then ")}` : ""
  return `Through session #${via[0].number}'s delegate task${then}${since ? `, since ${formatAbsolute(since)}` : ""}`
}

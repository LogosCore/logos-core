import { useLoaded } from "@/lib/preloadable"
import type { RecordingPlayerProps } from "./recording-player"
import { recordingPlayerCode } from "./recording-code"

/** A recording's player (recording-player.tsx), shown once its code is loaded. */
export function LazyRecordingPlayer(props: RecordingPlayerProps) {
  const code = useLoaded(recordingPlayerCode)
  if (!code) {
    return <div className="flex flex-1 items-center justify-center rounded-md border text-sm text-muted-foreground">Loading the player…</div>
  }
  return <code.RecordingPlayer {...props} />
}

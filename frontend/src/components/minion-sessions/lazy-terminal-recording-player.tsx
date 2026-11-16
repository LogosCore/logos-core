import { useLoaded } from "@/lib/preloadable"
import type { TerminalRecordingPlayerProps } from "./terminal-recording-player"
import { terminalRecordingPlayerCode } from "./terminal-recording-code"

/** A terminal recording's player (terminal-recording-player.tsx), shown once its code is loaded. */
export function LazyTerminalRecordingPlayer(props: TerminalRecordingPlayerProps) {
  const code = useLoaded(terminalRecordingPlayerCode)
  if (!code) {
    return <div className="flex flex-1 items-center justify-center rounded-md border text-sm text-muted-foreground">Loading the player…</div>
  }
  return <code.TerminalRecordingPlayer {...props} />
}

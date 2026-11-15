import { preloadable } from "@/lib/preloadable"

// A recording's player (Apache Guacamole's, vendored) is a chunk of its own:
// fetched when a desktop task's Recordings tab first shows
// (preloadRecordingPlayer), or when one plays. Use it through
// LazyRecordingPlayer (lazy-recording-player.tsx).

export const recordingPlayerCode = preloadable(() => import("./recording-player"))

/** Fetches the player's code in the background. A failure stays quiet; playing one reports it. */
export function preloadRecordingPlayer() {
  recordingPlayerCode.load().catch(() => {})
}

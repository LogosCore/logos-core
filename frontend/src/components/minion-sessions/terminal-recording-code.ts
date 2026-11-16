import { preloadable } from "@/lib/preloadable"

// A terminal recording's player (xterm.js) and its transcript (xterm.js
// without a screen) are chunks of their own: the player fetched when a
// terminal task's Recordings tab first shows (preloadTerminalRecordingPlayer),
// or when one plays; the transcript when one is downloaded. Use the player
// through LazyTerminalRecordingPlayer (lazy-terminal-recording-player.tsx).

export const terminalRecordingPlayerCode = preloadable(() => import("./terminal-recording-player"))

export const castTranscriptCode = preloadable(() => import("./cast-transcript"))

/** Fetches the player's code in the background. A failure stays quiet; playing one reports it. */
export function preloadTerminalRecordingPlayer() {
  terminalRecordingPlayerCode.load().catch(() => {})
}

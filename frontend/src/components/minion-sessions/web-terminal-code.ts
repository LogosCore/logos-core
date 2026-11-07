import { preloadable } from "@/lib/preloadable"

// The web terminal's code (xterm.js) is a chunk of its own: fetched when a
// terminal task's view first shows (preloadWebTerminal), or when a terminal
// opens. Use it through LazyWebTerminal (lazy-web-terminal.tsx).

export const webTerminalCode = preloadable(() => import("./web-terminal"))

/** Fetches the web terminal's code in the background. A failure stays quiet; opening one reports it. */
export function preloadWebTerminal() {
  webTerminalCode.load().catch(() => {})
}

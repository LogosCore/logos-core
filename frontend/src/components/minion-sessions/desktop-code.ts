import { preloadable } from "@/lib/preloadable"

// A desktop's code (Apache Guacamole's client, vendored) is a chunk of its
// own: fetched when a desktop task's view first shows (preloadDesktop), or
// when a desktop opens. Use it through LazyDesktop (lazy-desktop.tsx).

export const desktopCode = preloadable(() => import("./desktop-view"))

/** Fetches a desktop's code in the background. A failure stays quiet; opening one reports it. */
export function preloadDesktop() {
  desktopCode.load().catch(() => {})
}

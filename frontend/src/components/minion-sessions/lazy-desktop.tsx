import { useLoaded } from "@/lib/preloadable"
import type { DesktopViewProps } from "./desktop-view"
import { desktopCode } from "./desktop-code"

/** A desktop (desktop-view.tsx), shown once its code is loaded. */
export function LazyDesktop(props: DesktopViewProps) {
  const code = useLoaded(desktopCode)
  if (!code) {
    return <div className="flex flex-1 items-center justify-center rounded-md border text-sm text-muted-foreground">Loading the desktop…</div>
  }
  return <code.DesktopView {...props} />
}

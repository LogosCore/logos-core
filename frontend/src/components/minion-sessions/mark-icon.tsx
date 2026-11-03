import { preloadable, useLoaded } from "@/lib/preloadable"
import { cn } from "@/lib/utils"

// A session's mark icon (an emoji, or a wiki icon by name) where the Sessions
// page shows one: the table, the console's tab, the session's details. An
// emoji is text; an icon needs the wiki's DocumentIcon, and through it the
// two icon catalogs (roughly 1 MB of thunk tables), fetched the first time a
// mark with an icon shows, not with the page.

const iconCode = preloadable(() => import("@/components/wiki/document-icon"))

export function MarkIcon({ icon, emoji, size, className }: { icon?: string | null; emoji?: string | null; size: number; className?: string }) {
  if (!icon) {
    // As DocumentIcon draws an emoji.
    return emoji ? (
      <span className={cn("shrink-0 text-base leading-none", className)} aria-hidden>
        {emoji}
      </span>
    ) : null
  }
  return <LoadedIcon icon={icon} emoji={emoji} size={size} className={className} />
}

function LoadedIcon({ icon, emoji, size, className }: { icon: string; emoji?: string | null; size: number; className?: string }) {
  const code = useLoaded(iconCode)
  if (!code) return <span className={cn("inline-block shrink-0", className)} style={{ width: size, height: size }} aria-hidden />
  return <code.DocumentIcon icon={icon} emoji={emoji} size={size} className={className} />
}

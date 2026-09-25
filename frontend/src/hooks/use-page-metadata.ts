import { useEffect } from "react"
import type { LucideIcon } from "lucide-react"
import {
  ICON_LOOKUP,
  loadLucideIconAsync,
} from "@/components/wiki/icon-catalog"
import {
  STATIC_FAVICON_HREF,
  emojiToSvgDataUrl,
  lucideToSvgDataUrl,
  setFavicon,
} from "@/lib/favicon"

// `lucide-name` is for wiki documents whose icon string may be outside the
// curated catalog — those resolve async via the catalog's import-glob map,
// painting `fallbackEmoji` (or the static favicon) while the import lands.
export type PageIcon =
  | { kind: "lucide"; component: LucideIcon; color?: string | null }
  | {
      kind: "lucide-name"
      name: string
      color?: string | null
      fallbackEmoji?: string | null
    }
  | { kind: "emoji"; emoji: string }
  | { kind: "static" }

export interface PageMetadata {
  title: string
  icon: PageIcon
}

/**
 * Sets `document.title` and the favicon for the current page. The next
 * page's call overrides cleanly with no cleanup — intentional, avoids a
 * transient stale-title flicker on navigation.
 */
export function usePageMetadata(meta: PageMetadata): void {
  const { title, icon } = meta

  // Destructure into primitives for effect deps so we don't depend on the
  // re-created `icon` object identity each render.
  const kind = icon.kind
  const component = icon.kind === "lucide" ? icon.component : null
  const color =
    icon.kind === "lucide" || icon.kind === "lucide-name" ? icon.color : null
  const name = icon.kind === "lucide-name" ? icon.name : null
  const fallbackEmoji =
    icon.kind === "lucide-name" ? icon.fallbackEmoji ?? null : null
  const emoji = icon.kind === "emoji" ? icon.emoji : null

  useEffect(() => {
    document.title = title

    // Lucide icons render asynchronously (see lucideToSvgDataUrl), and an
    // uncurated one is imported first. The cancellation token prevents a
    // late result from clobbering a newer effect.
    let cancelled = false
    const paintLucide = (Icon: LucideIcon, color?: string | null) => {
      lucideToSvgDataUrl(Icon, color).then(
        (href) => {
          if (!cancelled) setFavicon(href)
        },
        (error) => console.error("Favicon render failed:", error),
      )
    }

    switch (icon.kind) {
      case "lucide":
        paintLucide(icon.component, icon.color)
        break
      case "lucide-name": {
        const curated = ICON_LOOKUP[icon.name]
        if (curated) {
          paintLucide(curated, icon.color)
          break
        }
        // Uncurated: paint the emoji-or-static fallback, then upgrade when
        // the async import lands.
        setFavicon(
          icon.fallbackEmoji
            ? emojiToSvgDataUrl(icon.fallbackEmoji)
            : STATIC_FAVICON_HREF,
        )
        loadLucideIconAsync(icon.name).then((Icon) => {
          if (!cancelled && Icon) paintLucide(Icon, icon.color)
        })
        break
      }
      case "emoji":
        setFavicon(emojiToSvgDataUrl(icon.emoji))
        break
      case "static":
        setFavicon(STATIC_FAVICON_HREF)
        break
    }

    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- icon is reconstructed each render; deps are its primitive parts.
  }, [title, kind, component, color, name, fallbackEmoji, emoji])
}

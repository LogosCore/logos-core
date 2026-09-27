import { useEffect } from "react"
import type { LucideIcon } from "lucide-react"
import {
  STATIC_FAVICON_HREF,
  emojiToSvgDataUrl,
  lucideToSvgDataUrl,
  setFavicon,
} from "@/lib/favicon"

// icon-catalog is imported dynamically, and only on the `lucide-name` branch.
//
// Every page calls this hook, so a static import put the catalog — the curated
// components plus a 1,940-entry glob thunk table, some 370 KB — in the chunk of
// each one. The login and enroll routes pay the most for it and use it least:
// they ask for `{ kind: "static" }` and nothing else, so 95% of what those two
// routes downloaded was a catalog they never read, in front of the first screen
// anyone sees.
//
// Only wiki documents store an arbitrary icon name, so `lucide-name` is the
// only branch that needs a lookup — and the wiki page imports the catalog
// directly anyway, which means the dynamic import resolves against a module
// already in flight there. Nothing is fetched twice.
//
// Resolving the curated name asynchronously costs a frame on the favicon. That
// was already the shape of this code: lucideToSvgDataUrl is async, so no branch
// here ever painted synchronously.
function loadIconCatalog() {
  return import("@/components/wiki/icon-catalog")
}

// A failed catalog fetch leaves the fallback favicon painted, which is the
// correct end state — log it rather than surfacing an unhandled rejection.
function noteCatalogFailure(error: unknown) {
  console.error("Favicon icon catalog failed to load:", error)
}

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
        // Paint the emoji-or-static fallback first, then upgrade once the
        // catalog — and, for an uncurated name, the icon's own chunk — lands.
        const { name, color, fallbackEmoji } = icon
        setFavicon(
          fallbackEmoji
            ? emojiToSvgDataUrl(fallbackEmoji)
            : STATIC_FAVICON_HREF,
        )
        loadIconCatalog().then(({ ICON_LOOKUP, loadLucideIconAsync }) => {
          if (cancelled) return
          const curated = ICON_LOOKUP[name]
          if (curated) {
            paintLucide(curated, color)
            return
          }
          loadLucideIconAsync(name).then((Icon) => {
            if (!cancelled && Icon) paintLucide(Icon, color)
          })
        }, noteCatalogFailure)
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

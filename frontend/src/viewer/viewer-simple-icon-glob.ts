// The HTML bundle's stand-in for components/wiki/simple-icon-glob.ts, swapped
// in by vite.viewer.config.ts.
//
// The app loads each brand icon as its own chunk. A classic-script bundle has
// no chunks, so the glob inlined all 3,000-odd SVGs — 5 MB parsed on every
// open, for icons most exports never show. Here the catalog gets the slug
// list (generated at build time, a few KB) and each loader pulls
// assets/simple-icons.js, which carries every SVG, the first time any brand
// icon renders.

import { SIMPLE_ICON_SLUGS } from "virtual:simple-icon-slugs"

declare global {
  interface Window {
    LogosSimpleIcons?: Record<string, string>
  }
}

let loading: Promise<Record<string, string>> | null = null

function loadAll(): Promise<Record<string, string>> {
  if (window.LogosSimpleIcons) return Promise.resolve(window.LogosSimpleIcons)
  loading ??= new Promise((resolve, reject) => {
    const script = document.createElement("script")
    script.src = "assets/simple-icons.js"
    script.async = true
    script.onload = () =>
      window.LogosSimpleIcons
        ? resolve(window.LogosSimpleIcons)
        : reject(new Error("assets/simple-icons.js did not load the icons"))
    script.onerror = () => {
      loading = null
      reject(new Error("assets/simple-icons.js is missing from the export"))
    }
    document.head.appendChild(script)
  })
  return loading
}

export const SVG_GLOB: Record<string, () => Promise<string>> = Object.fromEntries(
  SIMPLE_ICON_SLUGS.map((slug) => [`/simple-icons/${slug}.svg`, () => loadAll().then((all) => all[slug] ?? "")]),
)

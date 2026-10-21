import type { IconComponent } from "@/components/wiki/icon-catalog"

// lucide-react ships a broken `dynamicIconImports` entry that references `.ts`
// source files. The compiled icon modules are still one-file-per-icon under
// `./icons/<kebab>`, so we enumerate them via Vite's import.meta.glob and build
// our own working dynamic-import map. Each glob match becomes its own chunk,
// fetched only when the icon first renders.
//
// Both extensions are globbed on purpose: lucide emitted `.js` up to 1.6 and
// `.mjs` from 1.7 onward. Matching only one silently empties the extended icon
// library on the next dependency bump — which is exactly what happened when
// 1.6.0 moved to 1.35.0. `*.mjs` does not match the sibling `.mjs.map`
// sourcemaps, so no filtering is needed. ALL_LUCIDE_NAMES has a guard test.
//
// The `?lazy` query gives these imports module ids of their own, apart from
// the same files imported statically (the curated set above, and every
// `import { X } from "lucide-react"` in the app). Without it each of those
// icons is also a dynamic-import target, and a target has to be loadable on
// its own — so the bundler cut every statically used icon out of the code
// that uses it into a chunk of a few hundred bytes, and a page cost one
// request per icon. The price is that an icon loaded both ways is fetched
// twice, which is a few hundred bytes.
const ICON_GLOB: Record<string, () => Promise<{ default: IconComponent }>> = import.meta.glob<{ default: IconComponent }>(
  [
    "/node_modules/lucide-react/dist/esm/icons/*.mjs",
    "/node_modules/lucide-react/dist/esm/icons/*.js",
    "!/node_modules/lucide-react/dist/esm/icons/index.*",
  ],
  { query: "?lazy" },
)

export { ICON_GLOB }

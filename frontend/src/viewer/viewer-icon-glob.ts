// The HTML bundle's stand-in for components/wiki/icon-glob.ts, swapped in by
// vite.viewer.config.ts.
//
// The app loads each uncurated lucide icon as its own chunk on first use. A
// classic-script bundle has no chunks to load, so the same glob inlined every
// icon as a separate module — 5 MB of the viewer. lucide's `icons` export is
// the same set in one object at a fraction of that, and this hands it to the
// icon catalog in the glob's own shape: keys whose kebab-case base name maps
// back to the icon's PascalCase name, values that resolve to the component.

import { icons } from "lucide-react"
import type { IconComponent } from "@/components/wiki/icon-catalog"

/** "AArrowDown" → "a-arrow-down": splits before every capital, so the
 * catalog's kebabToPascal gives the name back exactly. */
function toKebab(name: string): string {
  return name.split(/(?=[A-Z])/).join("-").toLowerCase()
}

export const ICON_GLOB: Record<string, () => Promise<{ default: IconComponent }>> = Object.fromEntries(
  Object.entries(icons).map(([name, icon]) => [
    `/lucide/${toKebab(name)}.js`,
    () => Promise.resolve({ default: icon as unknown as IconComponent }),
  ]),
)

// Frequently-used icon row for the wiki document icon picker.
//
// The ranking lives on the server, on the caller's user (`me.frequentIcons`,
// written by `useRecordIconUse`), so the row follows the operator across
// browsers. It used to be kept in localStorage, which emptied it on every new
// browser, origin and cleared cache, and whenever the browser evicted site
// data on its own. What is left here is the read-side filter; the one-time
// import of the old key is in hooks/use-picker-history.ts, away from this
// module's icon catalog imports, because it runs in the app shell.

import { ALL_LUCIDE_NAMES, ICON_LOOKUP } from "@/components/wiki/icon-catalog"
import {
  hasSimpleIcon,
  isSimpleIconName,
  simpleIconSlug,
} from "@/components/wiki/simple-icon-catalog"

// Cap on rendered tiles — one row of 8 columns plus a second row gives users
// quick access without dominating the picker before they scroll. The server
// keeps 32, so an icon that drops out of this window is ranked, not forgotten.
const MAX_DISPLAYED = 16

// A stored name is either a bare lucide name or a brand icon encoded
// `si:<slug>`. Both are recorded on pick, so both have to be recognised on
// read — checking only the lucide catalog silently drops every brand icon a
// user picks, which is exactly what it did.
function isRenderable(name: string): boolean {
  if (isSimpleIconName(name)) return hasSimpleIcon(simpleIconSlug(name))
  return !!ICON_LOOKUP[name] || ALL_LUCIDE_NAMES.has(name)
}

/**
 * The names to show, from the server's ranking (most used first), keeping
 * only those still resolvable in the current bundles. Stale entries (an icon
 * removed or renamed upstream) are skipped silently so the picker never tries
 * to render a missing component.
 */
export function visibleFrequentIconNames(
  ranked: readonly string[],
  limit = MAX_DISPLAYED,
): string[] {
  const result: string[] = []
  for (const name of ranked) {
    if (!isRenderable(name)) continue
    result.push(name)
    if (result.length >= limit) break
  }
  return result
}

import { describe, expect, test } from "vitest"
import {
  SIMPLE_ICON_CATALOG,
  SIMPLE_ICON_PREFIX,
  allSimpleIconSlugs,
  hasSimpleIcon,
  isSimpleIconName,
  resolveSimpleIcon,
  simpleIconSlug,
  toSimpleIconName,
} from "@/components/wiki/simple-icon-catalog"

describe("simple-icon encoding", () => {
  test("isSimpleIconName only matches the si: prefix", () => {
    expect(isSimpleIconName("si:ubuntu")).toBe(true)
    // Bare lucide names and the adaptive sentinel are NOT simple icons —
    // this is the backward-compat contract that keeps old stored values lucide.
    expect(isSimpleIconName("Server")).toBe(false)
    expect(isSimpleIconName("Adaptive")).toBe(false)
    expect(isSimpleIconName("")).toBe(false)
    expect(isSimpleIconName(null)).toBe(false)
  })

  test("toSimpleIconName / simpleIconSlug round-trip", () => {
    expect(toSimpleIconName("ubuntu")).toBe(`${SIMPLE_ICON_PREFIX}ubuntu`)
    expect(simpleIconSlug(toSimpleIconName("ubuntu"))).toBe("ubuntu")
  })

  test("simpleIconSlug returns empty for non-prefixed values", () => {
    expect(simpleIconSlug("Server")).toBe("")
    expect(simpleIconSlug(null)).toBe("")
  })
})

describe("simple-icon registry", () => {
  test("the full slug set is populated from the package", () => {
    // Guards the import.meta.glob wiring: if the package layout changes and the
    // glob stops matching, this drops to 0 and the whole brand library silently
    // disappears from the pickers.
    expect(allSimpleIconSlugs().length).toBeGreaterThan(1000)
    expect(allSimpleIconSlugs()).toContain("ubuntu")
    expect(allSimpleIconSlugs()).toContain("linux")
  })

  test("allSimpleIconSlugs is sorted and memoized", () => {
    const first = allSimpleIconSlugs()
    // Same array identity on the second call — the corpus is built once. A
    // fresh sort per call would be a per-keystroke cost in the picker's search.
    expect(allSimpleIconSlugs()).toBe(first)
    expect([...first]).toEqual([...first].sort())
  })

  test("hasSimpleIcon agrees with the corpus without building it", () => {
    expect(hasSimpleIcon("ubuntu")).toBe(true)
    expect(hasSimpleIcon("definitely-not-a-real-brand-slug")).toBe(false)
  })

  test("every curated slug exists in the package (no blank tiles)", () => {
    const available = new Set(allSimpleIconSlugs())
    const missing = SIMPLE_ICON_CATALOG.flatMap((g) => g.icons)
      .map((i) => i.slug)
      .filter((slug) => !available.has(slug))
    expect(missing).toEqual([])
  })

  test("resolveSimpleIcon returns a component for a known slug, null otherwise", () => {
    expect(resolveSimpleIcon("ubuntu")).not.toBeNull()
    expect(resolveSimpleIcon("definitely-not-a-real-brand-slug")).toBeNull()
    expect(resolveSimpleIcon("")).toBeNull()
  })
})

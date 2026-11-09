import { describe, expect, it } from "vitest"
import { DEFAULT_FACTORY_ICON, defaultIconFor, iconMaskImage } from "./module-icon-values"

describe("iconMaskImage", () => {
  it("wraps the SVG in a data URL that survives CSS quoting", () => {
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 12h18" stroke='#000'/>\n</svg>`
    const css = iconMaskImage(svg)
    const prefix = 'url("data:image/svg+xml,'
    expect(css.startsWith(prefix) && css.endsWith('")')).toBe(true)
    const data = css.slice(prefix.length, -2)
    expect(data).not.toMatch(/["<>#\s]/)
    expect(decodeURIComponent(data)).toBe(svg)
  })
})

describe("defaultIconFor", () => {
  it("has one for each module type, and a fallback", () => {
    expect(defaultIconFor("channel")).toBe("channel")
    expect(defaultIconFor("factory")).toBe("factory")
    expect(defaultIconFor("something-new")).toBe("module")
  })
})

describe("DEFAULT_FACTORY_ICON", () => {
  it("names the app's own SVGs, in the 24-unit grid icons share", async () => {
    const { readFileSync } = await import("node:fs")
    const { resolve } = await import("node:path")
    for (const url of [DEFAULT_FACTORY_ICON.icon, DEFAULT_FACTORY_ICON.iconLight]) {
      expect(url.startsWith("/module-icons/")).toBe(true)
      const svg = readFileSync(resolve(__dirname, "../../../public", url.slice(1)), "utf8")
      expect(svg).toMatch(/^<svg xmlns="http:\/\/www\.w3\.org\/2000\/svg" viewBox="0 0 24 24"/)
      expect(svg).not.toMatch(/<script|on\w+=/i)
    }
  })
})

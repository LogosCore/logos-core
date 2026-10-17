import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"

import {
  BADGE_KEYS,
  BUILTIN_SHAPES,
  CARD_KINDS,
  COLOR_NAMES,
  EDGE_STYLES,
  ZONE_KINDS,
  buildCard,
  tagOf,
} from "./drawing-catalogue"

const read = (path: string) => readFileSync(new URL(path, import.meta.url), "utf8")

// The sidecar builds the same shapes for agents from its own copy, because its
// image is built from hocuspocus/ alone. A copy that drifted would draw an
// agent's cards differently from the panel's, and read the panel's wrongly.
describe("the sidecar's copy of the catalogue", () => {
  for (const file of ["drawing-catalogue.ts", "drawing-icons.generated.ts"]) {
    it(`${file} is identical; run npm run gen:drawing-icons after changing it`, () => {
      expect(read(`../../../../../hocuspocus/src/${file}`)).toBe(read(`./${file}`))
    })
  }
})

// The agents' guide lists every word the diagram language accepts. A kind
// added here and missing there is one no agent will ever use.
describe("the agents' drawing guide", () => {
  const guide = read("../../../../../core/pkg/mcp/skillassets/drawings.md")
  const words = [
    ...CARD_KINDS,
    ...BADGE_KEYS,
    ...Object.keys(EDGE_STYLES),
    ...Object.keys(ZONE_KINDS),
    ...COLOR_NAMES,
  ]
  for (const word of words) {
    it(`names \`${word}\``, () => {
      expect(guide).toContain(`\`${word}\``)
    })
  }
})

// A read tells a card from loose lines by these tags; an untagged part would
// be listed as a stray shape.
describe("tags", () => {
  it("every part of every built-in shape says what it is", () => {
    for (const shape of BUILTIN_SHAPES) {
      for (const el of shape.build("sketch")) {
        expect(tagOf(el), shape.name).not.toBeNull()
        const label = el.label as object | undefined
        if (label) expect(tagOf(label), `${shape.name} label`).not.toBeNull()
      }
    }
  })

  it("a card built for a diagram carries its id, kind and parts", () => {
    const card = buildCard({ kind: "server", title: "SRV", detail: "10.0.0.1", style: "clean", badges: ["flag"], node: "srv" })
    const parts = card.elements.map((el) => tagOf(el)?.part)
    expect(parts).toContain("frame")
    expect(parts).toContain("title")
    expect(parts).toContain("detail")
    expect(parts).toContain("badge:flag")
    for (const el of card.elements) expect(tagOf(el)).toMatchObject({ shape: "card:server", node: "srv", style: "clean" })
  })

  it("a card with no detail draws no empty line", () => {
    const card = buildCard({ kind: "server", title: "SRV", detail: "", style: "sketch" })
    expect(card.elements.filter((el) => el.type === "text").map((el) => el.text)).toEqual(["SRV"])
  })
})

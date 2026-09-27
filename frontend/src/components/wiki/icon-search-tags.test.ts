import { beforeAll, describe, expect, test } from "vitest"
import { ALL_LUCIDE_NAMES } from "@/components/wiki/icon-catalog"
import {
  iconSearchIndex,
  iconTagsLoaded,
  loadIconTags,
  searchIcons,
  tokenizeIconName,
} from "@/components/wiki/icon-search"
import LUCIDE_TAGS from "@/components/wiki/lucide-tags.generated"

// Separate from icon-search.test.ts because loading the tags is a one-way door
// for the module: that file pins the ranking that search has before the chunk
// arrives, this one what it gains after. Vitest isolates modules per file, so
// the two never see each other's state.

const search = (q: string, limit = 40) => searchIcons(q, limit).names

describe("the generated tag module", () => {
  test("names every icon by a name lucide actually ships", () => {
    // Drift guard. A lucide bump without `npm run gen:icon-tags` leaves this
    // file describing icons that no longer exist, and the entries it does not
    // cover silently lose their tags — neither shows up as a failure anywhere
    // else, because a missing tag is just a thinner search result.
    const unknown = Object.keys(LUCIDE_TAGS).filter(
      (name) => !ALL_LUCIDE_NAMES.has(name),
    )
    expect(unknown).toEqual([])
  })

  test("covers most of the installed set", () => {
    // 87.3% at lucide-react 1.35.0. A bump that drops coverage sharply means
    // the fetched version no longer matches the installed one.
    const coverage = Object.keys(LUCIDE_TAGS).length / ALL_LUCIDE_NAMES.size
    expect(coverage).toBeGreaterThan(0.85)
  })

  test("carries no tag that only repeats the icon's own name", () => {
    // What the generator trims. A tag the name already contains is bytes in
    // the chunk for recall the token tier gives us anyway, and it ranks lower.
    // Tokenized the same way search tokenizes, or this reads `NonBinary` as one
    // word and calls its "nonbinary" tag redundant when it is not.
    for (const [name, tags] of Object.entries(LUCIDE_TAGS)) {
      const own = new Set(tokenizeIconName(name))
      for (const tag of tags) {
        const words = tag.split(" ")
        expect(words.every((w) => own.has(w)), `${name} / ${tag}`).toBe(false)
      }
    }
  })

  test("is lowercase and free of empties", () => {
    for (const tags of Object.values(LUCIDE_TAGS)) {
      for (const tag of tags) {
        expect(tag).toBe(tag.toLowerCase().trim())
        expect(tag.length).toBeGreaterThan(0)
      }
    }
  })
})

describe("search before the tags load", () => {
  test("works, and says the tags are not in yet", () => {
    expect(iconTagsLoaded()).toBe(false)
    // The name and synonym tiers carry it on their own.
    expect(search("server")[0]).toBe("Server")
    expect(search("vulnerability")).toContain("Bug")
    // And this is what it cannot answer yet.
    expect(search("vulnerability")).not.toContain("ShieldAlert")
  })
})

describe("search with the tags", () => {
  beforeAll(async () => {
    await loadIconTags()
  })

  test("the index is rebuilt with them", () => {
    expect(iconTagsLoaded()).toBe(true)
    const terminal = iconSearchIndex().find((e) => e.name === "Terminal")
    expect(terminal?.tags).toContain("command line")
    // Each word of a multi-word tag is matchable on its own.
    expect(terminal?.tags).toContain("command")
  })

  test("reaches icons whose names say nothing about the query", () => {
    expect(search("vulnerability")).toContain("ShieldAlert")
    expect(search("password")).toContain("KeyRound")
    expect(search("password")).toContain("SquareAsterisk")
    expect(search("spreadsheet")).toContain("Table")
    expect(search("authentication")).toContain("Key")
  })

  test("leaves the brand icons untagged", () => {
    // Simple Icons ships titles and aliases, not tags — nothing to merge here,
    // and a brand entry claiming tags would mean the generator matched a slug
    // to a lucide name.
    for (const entry of iconSearchIndex()) {
      if (entry.name.startsWith("si:")) expect(entry.tags).toEqual([])
    }
  })

  test("tag matches rank below name and keyword matches", () => {
    // The reason the tag tier sits where it does: lucide tags "shell" onto
    // `Egg` and `Shrimp`, so breakfast has to land under the terminals.
    const names = search("shell", 96)
    expect(names[0]).toBe("Shell")
    for (const shellish of ["Terminal", "SquareTerminal"]) {
      expect(names.indexOf(shellish)).toBeLessThan(names.indexOf("Egg"))
      expect(names.indexOf(shellish)).toBeLessThan(names.indexOf("Shrimp"))
    }
  })

  test("does not displace an exact name", () => {
    // Tags are numerous enough to swamp the ranking if they outranked names:
    // `Bug` is tagged onto a dozen icons, and typing "bug" still means `Bug`.
    for (const name of ["Bug", "Key", "Table", "Image", "Flag"]) {
      expect(search(name, 1)[0], name).toBe(name)
    }
  })
})

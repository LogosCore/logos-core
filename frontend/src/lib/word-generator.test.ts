import { describe, expect, it } from "vitest"
import {
  candidates,
  drawRandomness,
  drawsFrom,
  fromDrawn,
  formatCombinations,
  generate,
  log10Combinations,
  PRESETS,
  pool,
  randomIndex,
  settingsOf,
  type GeneratorSettings,
  type Random,
  type WordlistInfo,
} from "./word-generator"

/** Random numbers in turn, from the start again when they run out. */
function sequence(...xs: number[]): Random {
  let i = 0
  return () => xs[i++ % xs.length]
}

const path = PRESETS.path
const pools = { first: ["quiet", "bold"], last: ["river", "stone", "cloud"] }
const set = (over: Partial<GeneratorSettings>): GeneratorSettings => ({ ...path, ...over })

const catalog: WordlistInfo[] = [
  { id: "common", name: "Common words", kind: "word", count: 3780, sources: ["scowl"] },
  { id: "adjectives", name: "Adjectives", kind: "adjective", count: 2161, sources: ["corpora"] },
  { id: "colors", name: "Colors", kind: "adjective", count: 90, sources: ["ung"] },
  { id: "nouns", name: "Nouns", kind: "noun", count: 3337, sources: ["corpora"] },
  { id: "theme-coding", name: "Coding", kind: "noun", theme: true, count: 70, sources: ["imsky"] },
]

describe("randomIndex", () => {
  it("draws again above the last whole multiple of n, so every index is as likely", () => {
    // 2^32 % 3 = 1: the last draw, 2^32 - 1, would favour 0.
    expect(randomIndex(3, sequence(2 ** 32 - 1, 7))).toBe(1)
    expect(randomIndex(1, sequence(123))).toBe(0)
    expect(() => randomIndex(0)).toThrow()
  })

  it("draws from crypto by default", () => {
    for (let i = 0; i < 50; i++) expect(randomIndex(10)).toBeLessThan(10)
  })

  it("reads numbers drawn ahead in turn, the same for each reader", () => {
    const drawn = drawRandomness(8)
    const a = fromDrawn(drawn)
    const b = fromDrawn(drawn)
    const first = [a(), a(), a()]
    expect([b(), b(), b()]).toEqual(first)
    expect(first).toEqual([drawn[0], drawn[1], drawn[2]])
  })
})

describe("generate", () => {
  it("makes a path of words by default: segments between dividers, one before and after", () => {
    // Indexes 0, 1 of the last pool: river, stone.
    expect(generate(path, pools, sequence(0, 1))).toBe("/river/stone/")
  })

  it("joins a segment's words, adjectives before the noun", () => {
    const s = set({ segments: 1, words: 3, joiner: "-", leading: false, trailing: false })
    expect(generate(s, pools, sequence(1, 0, 2))).toBe("bold-quiet-cloud")
    expect(generate({ ...s, joiner: "_" }, pools, sequence(1, 0, 2))).toBe("bold_quiet_cloud")
    expect(generate({ ...s, joiner: "" }, pools, sequence(1, 0, 2))).toBe("boldquietcloud")
    expect(generate({ ...s, joiner: "camel" }, pools, sequence(1, 0, 2))).toBe("boldQuietCloud")
    expect(generate({ ...s, joiner: "camel", case: "title" }, pools, sequence(1, 0, 2))).toBe("BoldQuietCloud")
    expect(generate({ ...s, case: "title" }, pools, sequence(1, 0, 2))).toBe("Bold-Quiet-Cloud")
  })

  it("ends the last segment with a number of all its digits", () => {
    // 1000 + 3821: four digits, never fewer.
    expect(generate(set({ segments: 1, digits: 4 }), pools, sequence(0, 3821))).toBe("/river-4821/")
    expect(generate(set({ segments: 2, digits: 2, joiner: "", divider: "." , leading: false }), pools, sequence(0, 1, 89))).toBe("river.stone99.")
  })

  it("makes nothing from an empty pool it needs", () => {
    expect(generate(path, { first: [], last: [] })).toBeNull()
    expect(generate(set({ words: 2 }), { first: [], last: ["river"] })).toBeNull()
    expect(generate(set({ words: 1 }), { first: [], last: ["river"] })).toBe("/river/river/")
  })
})

describe("candidates", () => {
  it("are different values that fit the field", () => {
    const got = candidates(set({ segments: 1 }), pools, 5, (v) => v !== "/stone/", sequence(0, 1, 2, 0, 1, 2))
    expect(got).toEqual(["/river/", "/cloud/"])
  })

  it("give up on settings whose values never fit", () => {
    expect(candidates(path, pools, 5, () => false)).toEqual([])
  })
})

describe("drawsFrom", () => {
  it("draws any words from every list picked, ids the catalog lacks left out", () => {
    expect(drawsFrom(set({ lists: ["common", "gone"] }), catalog)).toEqual({ first: ["common"], last: ["common"] })
  })

  it("draws adjectives then a noun from the picked lists of each kind, or the kind's main list", () => {
    expect(drawsFrom(set({ pattern: "adjective-noun", lists: ["colors", "theme-coding", "common"] }), catalog)).toEqual({
      first: ["colors"],
      last: ["theme-coding"],
    })
    expect(drawsFrom(set({ pattern: "adjective-noun", lists: ["common"] }), catalog)).toEqual({
      first: ["adjectives"],
      last: ["nouns"],
    })
  })

  it("pools lists' words, each once", () => {
    expect(pool([["a", "b"], ["b", "c"]])).toEqual(["a", "b", "c"])
  })
})

describe("combinations", () => {
  it("count every choice: words per segment, segments, and the number", () => {
    // (2 × 3)^2 segments = 36; × 90 two-digit numbers = 3240.
    const s = set({ segments: 2, words: 2, digits: 2 })
    expect(10 ** log10Combinations(s, { first: 2, last: 3 })).toBeCloseTo(3240)
    expect(log10Combinations(s, { first: 0, last: 3 })).toBe(-Infinity)
  })

  it("read in full below a million, else as a power of ten", () => {
    expect(formatCombinations(Math.log10(3240))).toBe("3,240")
    expect(formatCombinations(Math.log10(4.3e9))).toBe("4.3 × 10⁹")
    expect(formatCombinations(Math.log10(9.99e11))).toBe("1.0 × 10¹²")
    expect(formatCombinations(-Infinity)).toBe("0")
  })
})

describe("settingsOf", () => {
  it("keeps what was saved, and takes the preset's for anything missing or out of range", () => {
    expect(settingsOf({ segments: 4, divider: ".", words: 9, joiner: "?", lists: [] }, path)).toEqual({
      ...path,
      segments: 4,
      divider: ".",
    })
    expect(settingsOf(null, path)).toEqual(path)
    expect(settingsOf({ pattern: "adjective-noun", lists: ["nouns"], digits: 3 }, path)).toMatchObject({
      pattern: "adjective-noun",
      lists: ["nouns"],
      digits: 3,
    })
  })
})

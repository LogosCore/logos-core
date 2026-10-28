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
const words = ["river", "stone", "cloud"]
const set = (over: Partial<GeneratorSettings>): GeneratorSettings => ({ ...path, ...over })

const catalog: WordlistInfo[] = [
  { id: "common", name: "Common words", count: 3780, sources: ["scowl", "corpora"] },
  { id: "short", name: "Short words", count: 1293, sources: ["eff"] },
  { id: "long", name: "Long word list", count: 7768, sources: ["eff"] },
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
    expect(generate(path, words, sequence(0, 1))).toBe("/river/stone/")
  })

  it("joins a segment's words, and cases them", () => {
    const s = set({ segments: 1, words: 3, joiner: "-", leading: false, trailing: false })
    expect(generate(s, words, sequence(1, 0, 2))).toBe("stone-river-cloud")
    expect(generate({ ...s, joiner: "_" }, words, sequence(1, 0, 2))).toBe("stone_river_cloud")
    expect(generate({ ...s, joiner: "" }, words, sequence(1, 0, 2))).toBe("stonerivercloud")
    expect(generate({ ...s, joiner: "camel" }, words, sequence(1, 0, 2))).toBe("stoneRiverCloud")
    expect(generate({ ...s, joiner: "camel", case: "title" }, words, sequence(1, 0, 2))).toBe("StoneRiverCloud")
    expect(generate({ ...s, case: "title" }, words, sequence(1, 0, 2))).toBe("Stone-River-Cloud")
  })

  it("ends the last segment with a number of all its digits", () => {
    // 1000 + 3821: four digits, never fewer.
    expect(generate(set({ segments: 1, digits: 4 }), words, sequence(0, 3821))).toBe("/river-4821/")
    expect(generate(set({ segments: 2, digits: 2, joiner: "", divider: ".", leading: false }), words, sequence(0, 1, 89))).toBe(
      "river.stone99.",
    )
  })

  it("makes nothing without words", () => {
    expect(generate(path, [])).toBeNull()
  })
})

describe("candidates", () => {
  it("are different values that fit the field", () => {
    const got = candidates(set({ segments: 1 }), words, 5, (v) => v !== "/stone/", sequence(0, 1, 2, 0, 1, 2))
    expect(got).toEqual(["/river/", "/cloud/"])
  })

  it("give up on settings whose values never fit", () => {
    expect(candidates(path, words, 5, () => false)).toEqual([])
  })
})

describe("drawsFrom", () => {
  it("draws on the lists picked that the catalog has", () => {
    expect(drawsFrom(set({ lists: ["short", "gone", "long"] }), catalog)).toEqual(["short", "long"])
  })

  it("draws on the catalog's first list when none picked is there any more", () => {
    expect(drawsFrom(set({ lists: ["theme-coding"] }), catalog)).toEqual(["common"])
    expect(drawsFrom(path, [])).toEqual([])
  })

  it("pools lists' words, each once", () => {
    expect(pool([["a", "b"], ["b", "c"]])).toEqual(["a", "b", "c"])
  })
})

describe("combinations", () => {
  it("count every choice: words per segment, segments, and the number", () => {
    // 3 words, 2 in each of 2 segments: 3^4 = 81; × 90 two-digit numbers = 7290.
    const s = set({ segments: 2, words: 2, digits: 2 })
    expect(10 ** log10Combinations(s, 3)).toBeCloseTo(7290)
    expect(log10Combinations(s, 0)).toBe(-Infinity)
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
    // A setting it no longer has is dropped.
    expect(settingsOf({ pattern: "adjective-noun", lists: ["long"], digits: 3 }, path)).toEqual({
      ...path,
      lists: ["long"],
      digits: 3,
    })
  })
})

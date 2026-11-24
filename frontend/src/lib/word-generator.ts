// The value generator's logic (components/modules/management/word-generator.tsx):
// values made of words from the shipped word lists (public/wordlists, built
// by scripts/generate-wordlists.mjs), in a shape the operator sets, such as
// /quiet/river/ for a path. No React here, so all of it is unit-tested.

/** What goes between segments, and before and after them when asked. */
export type Divider = "/" | "-" | "." | "_"
export type LetterCase = "lower" | "title"

/** The fewest and the most segments a value can have, both counted. */
export type SegmentRange = readonly [fewest: number, most: number]

/**
 * The ranges of segments the slider steps through: a value has one of a
 * range's counts, each as likely, so the values made are not all as long.
 */
export const SEGMENT_RANGES: readonly SegmentRange[] = [
  [2, 5],
  [3, 6],
  [4, 7],
  [5, 8],
]

export interface GeneratorSettings {
  /** How many words, one a segment: one of SEGMENT_RANGES. */
  segments: SegmentRange
  divider: Divider
  /** A divider before the first segment ("/quiet"), and after the last ("river/"). */
  leading: boolean
  trailing: boolean
  case: LetterCase
  /** The word lists picked, by id: every word comes from them all. None: every list the catalog has. */
  lists: string[]
}

/** What a word list is, as public/wordlists/index.json lists it. */
export interface WordlistInfo {
  id: string
  name: string
  count: number
  /** Where its words come from, by source id. */
  sources: string[]
}

export interface WordlistSource {
  name: string
  url: string
  license: string
}

export interface WordlistCatalog {
  lists: WordlistInfo[]
  sources: Record<string, WordlistSource>
}

/** How a field of each format starts, before the operator changes anything. */
export const PRESETS: Record<string, GeneratorSettings> = {
  path: {
    segments: SEGMENT_RANGES[0],
    divider: "/",
    leading: true,
    trailing: true,
    case: "lower",
    // Every list, one added to the catalog later too.
    lists: [],
  },
}

/** Formats the generator serves. */
export const FORMATS = Object.keys(PRESETS)

/**
 * Settings kept from an earlier visit, made whole and safe: anything missing
 * or out of range takes the preset's.
 */
export function settingsOf(saved: unknown, preset: GeneratorSettings): GeneratorSettings {
  const s = typeof saved === "object" && saved !== null ? (saved as Partial<Record<keyof GeneratorSettings, unknown>>) : {}
  const oneOf = <T>(v: unknown, options: readonly T[], fallback: T): T => (options.includes(v as T) ? (v as T) : fallback)
  const range = s.segments
  return {
    // One of the slider's ranges, as SEGMENT_RANGES has it.
    segments:
      SEGMENT_RANGES.find((r) => Array.isArray(range) && range.length === 2 && r[0] === range[0] && r[1] === range[1]) ??
      preset.segments,
    divider: oneOf<Divider>(s.divider, ["/", "-", ".", "_"], preset.divider),
    leading: typeof s.leading === "boolean" ? s.leading : preset.leading,
    trailing: typeof s.trailing === "boolean" ? s.trailing : preset.trailing,
    case: oneOf<LetterCase>(s.case, ["lower", "title"], preset.case),
    lists:
      Array.isArray(s.lists) && s.lists.length > 0 && s.lists.every((id) => typeof id === "string")
        ? (s.lists as string[])
        : preset.lists,
  }
}

/**
 * The lists a value's words come from: those picked that the catalog has,
 * or every list it has when none is picked (the preset) or none of those
 * picked is there any more (picked once and gone since).
 */
export function drawsFrom(s: GeneratorSettings, catalog: readonly WordlistInfo[]): string[] {
  const picked = s.lists.filter((id) => catalog.some((l) => l.id === id))
  return picked.length > 0 ? picked : catalog.map((l) => l.id)
}

/** Words of several lists as one pool, each word once. */
export function pool(lists: readonly (readonly string[])[]): string[] {
  return [...new Set(lists.flat())]
}

/** A source of random 32-bit unsigned integers. */
export type Random = () => number

/** crypto's random numbers, one at a time. */
export const cryptoRandom: Random = () => crypto.getRandomValues(new Uint32Array(1))[0]

/**
 * Random numbers drawn ahead, for a component: drawn in an event (opening,
 * Shuffle), then read while rendering, which must stay pure. 4096 cover the
 * five candidates with room for the tries that do not fit.
 */
export function drawRandomness(size = 4096): Uint32Array {
  return crypto.getRandomValues(new Uint32Array(size))
}

/** A Random reading drawn numbers in turn, from the start for every value it is made for. */
export function fromDrawn(drawn: Uint32Array): Random {
  let i = 0
  return () => drawn[i++ % drawn.length]
}

const RANGE = 2 ** 32

/** A random integer from 0 to n - 1, every one as likely: draws above the last whole multiple of n are drawn again. */
export function randomIndex(n: number, random: Random = cryptoRandom): number {
  if (!Number.isInteger(n) || n < 1 || n > RANGE) throw new Error(`randomIndex: ${n} is not a count of choices`)
  const limit = RANGE - (RANGE % n)
  for (;;) {
    const x = random()
    if (x < limit) return x % n
  }
}

const title = (w: string) => (w === "" ? w : w[0].toUpperCase() + w.slice(1))

/**
 * One value in the shape s sets, drawn from words (drawsFrom's lists, merged
 * by pool): first how many segments, each count of the range as likely, then
 * a word a segment. Null when there are none.
 */
export function generate(s: GeneratorSettings, words: readonly string[], random: Random = cryptoRandom): string | null {
  if (words.length === 0) return null
  const [fewest, most] = s.segments
  const count = fewest + randomIndex(most - fewest + 1, random)
  const segments: string[] = []
  for (let i = 0; i < count; i++) {
    const w = words[randomIndex(words.length, random)]
    segments.push(s.case === "title" ? title(w) : w)
  }
  return (s.leading ? s.divider : "") + segments.join(s.divider) + (s.trailing ? s.divider : "")
}

/**
 * Up to count different values that fit the field (fits), drawn until found
 * or after enough tries: settings no value of which fits give none.
 */
export function candidates(
  s: GeneratorSettings,
  words: readonly string[],
  count: number,
  fits: (value: string) => boolean,
  random: Random = cryptoRandom,
): string[] {
  const out = new Set<string>()
  for (let tries = 0; tries < count * 40 && out.size < count; tries++) {
    const v = generate(s, words, random)
    if (v === null) break
    if (fits(v)) out.add(v)
  }
  return [...out]
}

/**
 * How many different values the settings can make from size words, as its
 * base-10 logarithm: size^fewest + … + size^most, which for a big list
 * passes any number's range. -Infinity without words.
 */
export function log10Combinations(s: GeneratorSettings, size: number): number {
  if (size === 0) return -Infinity
  const [fewest, most] = s.segments
  // size^most × (1 + 1/size + … + 1/size^(most - fewest)): the sum's small part summed.
  let rest = 0
  for (let k = 0; k <= most - fewest; k++) rest += size ** -k
  return most * Math.log10(size) + Math.log10(rest)
}

const SUPERSCRIPT = "⁰¹²³⁴⁵⁶⁷⁸⁹"

/** A count from its logarithm: in full below a million ("48,600"), else "4.3 × 10⁹". */
export function formatCombinations(log10: number): string {
  if (log10 === -Infinity) return "0"
  if (log10 < 6) return Math.round(10 ** log10).toLocaleString("en-US")
  let exponent = Math.floor(log10)
  let mantissa = 10 ** (log10 - exponent)
  if (mantissa >= 9.95) {
    mantissa = 1
    exponent++
  }
  const power = String(exponent)
    .split("")
    .map((d) => SUPERSCRIPT[Number(d)])
    .join("")
  return `${mantissa.toFixed(1)} × 10${power}`
}

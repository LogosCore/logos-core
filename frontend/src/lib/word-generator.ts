// The value generator's logic (components/modules/management/word-generator.tsx):
// values made of words from the shipped word lists (public/wordlists, built
// by scripts/generate-wordlists.mjs), in a shape the operator sets, such as
// /quiet/river/ for a path. No React here, so all of it is unit-tested.

/** How words join inside a segment: "quiet-river", "quiet_river", "quietriver", "quietRiver". */
export type Joiner = "-" | "_" | "" | "camel"
/** What goes between segments, and before and after them when asked. */
export type Divider = "/" | "-" | "." | "_"
export type LetterCase = "lower" | "title"
/** Any words from the lists picked, or adjectives then a noun in each segment. */
export type WordPattern = "any" | "adjective-noun"

export interface GeneratorSettings {
  /** 1 to MAX_SEGMENTS. */
  segments: number
  /** Words in each segment, 1 to MAX_WORDS. */
  words: number
  joiner: Joiner
  divider: Divider
  /** A divider before the first segment ("/quiet"), and after the last ("river/"). */
  leading: boolean
  trailing: boolean
  case: LetterCase
  /** A number of this many digits ends the last segment ("river-4821"); 0 for none. */
  digits: number
  pattern: WordPattern
  /** The word lists picked, by id. */
  lists: string[]
}

export const MAX_SEGMENTS = 6
export const MAX_WORDS = 3
export const MAX_DIGITS = 6

/** What a word list is, as public/wordlists/index.json lists it. */
export interface WordlistInfo {
  id: string
  name: string
  /** For the adjective + noun pattern; "word" and "verb" lists take no part in it. */
  kind: "word" | "adjective" | "noun" | "verb"
  /** One of imsky's nouns by theme. */
  theme?: boolean
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
    segments: 2,
    words: 1,
    joiner: "-",
    divider: "/",
    leading: true,
    trailing: true,
    case: "lower",
    digits: 0,
    pattern: "any",
    lists: ["common"],
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
  const int = (v: unknown, min: number, max: number, fallback: number) =>
    typeof v === "number" && Number.isInteger(v) && v >= min && v <= max ? v : fallback
  const oneOf = <T>(v: unknown, options: readonly T[], fallback: T): T => (options.includes(v as T) ? (v as T) : fallback)
  return {
    segments: int(s.segments, 1, MAX_SEGMENTS, preset.segments),
    words: int(s.words, 1, MAX_WORDS, preset.words),
    joiner: oneOf<Joiner>(s.joiner, ["-", "_", "", "camel"], preset.joiner),
    divider: oneOf<Divider>(s.divider, ["/", "-", ".", "_"], preset.divider),
    leading: typeof s.leading === "boolean" ? s.leading : preset.leading,
    trailing: typeof s.trailing === "boolean" ? s.trailing : preset.trailing,
    case: oneOf<LetterCase>(s.case, ["lower", "title"], preset.case),
    digits: int(s.digits, 0, MAX_DIGITS, preset.digits),
    pattern: oneOf<WordPattern>(s.pattern, ["any", "adjective-noun"], preset.pattern),
    lists:
      Array.isArray(s.lists) && s.lists.length > 0 && s.lists.every((id) => typeof id === "string")
        ? (s.lists as string[])
        : preset.lists,
  }
}

/**
 * The lists a value's words come from. With any words, all of them from
 * every list picked. With adjective + noun, the last word of a segment from
 * the noun lists picked and the words before it from the adjective lists
 * picked; a kind with none picked takes its main list ("adjectives",
 * "nouns"). Ids the catalog does not have are left out.
 */
export function drawsFrom(s: GeneratorSettings, catalog: readonly WordlistInfo[]): { first: string[]; last: string[] } {
  const known = new Map(catalog.map((l) => [l.id, l]))
  const picked = s.lists.filter((id) => known.has(id))
  if (s.pattern === "any") return { first: picked, last: picked }
  const ofKind = (kind: WordlistInfo["kind"], main: string) => {
    const lists = picked.filter((id) => known.get(id)?.kind === kind)
    return lists.length > 0 ? lists : known.has(main) ? [main] : []
  }
  return { first: ofKind("adjective", "adjectives"), last: ofKind("noun", "nouns") }
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

/** A segment's words, cased and joined. */
function segment(words: readonly string[], s: GeneratorSettings): string {
  if (s.joiner === "camel") {
    return words.map((w, i) => (i === 0 && s.case === "lower" ? w : title(w))).join("")
  }
  return words.map((w) => (s.case === "title" ? title(w) : w)).join(s.joiner)
}

/**
 * One value in the shape s sets, its words drawn from the pools (drawsFrom's
 * lists, merged by pool). Null when a pool it needs is empty.
 */
export function generate(
  s: GeneratorSettings,
  pools: { first: readonly string[]; last: readonly string[] },
  random: Random = cryptoRandom,
): string | null {
  if (pools.last.length === 0 || (s.words > 1 && pools.first.length === 0)) return null
  const segments: string[] = []
  for (let i = 0; i < s.segments; i++) {
    const words: string[] = []
    for (let j = 0; j < s.words; j++) {
      const from = j === s.words - 1 ? pools.last : pools.first
      words.push(from[randomIndex(from.length, random)])
    }
    segments.push(segment(words, s))
  }
  if (s.digits > 0) {
    // No leading zero: the number has all its digits.
    const low = 10 ** (s.digits - 1)
    const n = low + randomIndex(9 * low, random)
    const glue = s.joiner === "-" || s.joiner === "_" ? s.joiner : ""
    segments[segments.length - 1] += glue + String(n)
  }
  return (s.leading ? s.divider : "") + segments.join(s.divider) + (s.trailing ? s.divider : "")
}

/**
 * Up to count different values that fit the field (fits), drawn until found
 * or after enough tries: settings no value of which fits give none.
 */
export function candidates(
  s: GeneratorSettings,
  pools: { first: readonly string[]; last: readonly string[] },
  count: number,
  fits: (value: string) => boolean,
  random: Random = cryptoRandom,
): string[] {
  const out = new Set<string>()
  for (let tries = 0; tries < count * 40 && out.size < count; tries++) {
    const v = generate(s, pools, random)
    if (v === null) break
    if (fits(v)) out.add(v)
  }
  return [...out]
}

/**
 * How many different values the settings can make, as its base-10
 * logarithm: the count passes any number's range long before the sliders
 * do. -Infinity when a pool it needs is empty.
 */
export function log10Combinations(s: GeneratorSettings, sizes: { first: number; last: number }): number {
  if (sizes.last === 0 || (s.words > 1 && sizes.first === 0)) return -Infinity
  const perSegment = (s.words - 1) * Math.log10(sizes.first) + Math.log10(sizes.last)
  const number = s.digits > 0 ? Math.log10(9) + (s.digits - 1) : 0
  return s.segments * perSegment + number
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

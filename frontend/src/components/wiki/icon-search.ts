// Ranked search over every icon the picker can offer.
//
// What this replaces: four independent `includes(query)` filters — curated
// lucide, the ~1900 uncurated lucide names, curated brand slugs, the ~3400
// uncurated ones — each rendered as its own section in that fixed order. Two
// things were wrong with it. Results arrived in glob order, so "server" led
// with whatever the bundler happened to enumerate first rather than with
// `Server`; and the sections were ordered by source, so "ubuntu" put its one
// real answer below a "More icons" heading the query had nothing to do with.
//
// The uncurated names also matched on the name alone. The curated 135 carry
// hand-written keywords, so "shell" finds `Terminal` — but nothing carried
// them for the rest of the set, which is where the icon a search is actually
// looking for usually lives.
//
// So: one ranked list, and keywords for everything. The keywords are attached
// to NAME TOKENS rather than to icons (SYNONYMS below), which is what makes
// covering the full set tractable — one `terminal` entry serves `Terminal`,
// `SquareTerminal` and `TerminalSquare` alike, and keeps serving whatever
// lucide adds next with that word in its name.

import {
  ALL_LUCIDE_NAMES,
  ICON_CATALOG,
  ICON_LOOKUP,
} from "@/components/wiki/icon-catalog"
import {
  allSimpleIconSlugs,
  CURATED_SIMPLE_SLUGS,
  SIMPLE_ICON_CATALOG,
  simpleIconSlug,
  toSimpleIconName,
} from "@/components/wiki/simple-icon-catalog"

/**
 * Splits an icon name into lowercase words: `ServerCog` → `server`, `cog`.
 *
 * Handles the three shapes the two libraries produce — PascalCase lucide names
 * (`ServerCog`), leading acronyms (`AArrowDown` → `a`, `arrow`, `down`;
 * `QrCode` → `qr`, `code`), and trailing digits, which lucide uses for
 * variants and must split off so `Columns3` answers to "columns"
 * (`Volume2` → `volume`, `2`). Brand slugs are already lowercase and mostly
 * one word, but the digit rule earns its keep there too: `1password` →
 * `1`, `password`.
 */
export function tokenizeIconName(name: string): readonly string[] {
  // simpleIconSlug answers "" for anything unprefixed, which is every lucide
  // name — so this is the prefix check and the strip in one step.
  const bare = simpleIconSlug(name) || name
  // Alternation order matters: the acronym branch has to be tried before the
  // ordinary-word branch, or `AArrowDown` would take `Ar` and lose the `A`.
  const matches = bare.match(/[A-Z]+(?![a-z])|[A-Z][a-z]*|[a-z]+|[0-9]+/g)
  return matches ? matches.map((m) => m.toLowerCase()) : []
}

/**
 * Extra search terms per name token, applied to every icon whose name contains
 * that token — curated or not.
 *
 * Entries earn their place by being a word an operator types that the icon's
 * own name does not contain: "vulnerability" for `Bug`, "wireless" for `Wifi`,
 * "ntlm" for `Hash`. A synonym that merely restates the token adds noise to
 * every sibling name, so tokens too generic to mean anything on their own
 * (`circle`, `square`, `round`, the directions) are deliberately absent.
 *
 * Every key must be a real token of some lucide icon; icon-search.test.ts
 * fails on one that is not, which is what catches a typo here and a lucide
 * rename that drops a word out of the set.
 */
export const SYNONYMS: Readonly<Record<string, readonly string[]>> = {
  // Files, docs and folders
  file: ["doc", "document"],
  folder: ["directory", "dir"],
  clipboard: ["paste", "checklist"],
  scroll: ["script", "parchment"],
  book: ["manual", "guide", "docs"],
  library: ["books", "collection"],
  newspaper: ["news", "article", "press"],
  sticky: ["memo", "note"],
  quote: ["citation", "blockquote"],
  paperclip: ["attach", "attachment"],
  printer: ["print"],
  archive: ["zip", "tar", "backup"],

  // Security
  lock: ["secure", "private", "password"],
  unlock: ["insecure", "open"],
  key: ["password", "secret", "credential", "auth"],
  shield: ["security", "protect", "defence", "defense"],
  bug: ["vulnerability", "issue", "defect", "malware"],
  skull: ["dead", "danger", "malware", "pwned"],
  worm: ["malware", "virus"],
  ghost: ["stealth", "hidden", "opsec"],
  fingerprint: ["identity", "biometric", "forensics"],
  footprints: ["tracks", "trail", "forensics"],
  syringe: ["inject", "injection", "payload"],
  eye: ["view", "watch", "visible", "surveil"],
  radar: ["scan", "sweep", "detect", "recon"],
  scan: ["detect", "discover", "recon"],
  crosshair: ["target", "aim"],
  target: ["goal", "objective", "aim"],
  hash: ["md5", "sha", "ntlm", "digest"],
  binary: ["bits", "hex", "data"],

  // Hosts and network
  server: ["host", "machine", "box"],
  database: ["db", "sql", "datastore"],
  drive: ["disk", "storage", "volume"],
  network: ["lan", "topology", "subnet"],
  wifi: ["wireless", "wlan"],
  router: ["gateway", "hop"],
  cable: ["ethernet", "wire", "patch"],
  globe: ["world", "internet", "web", "dns"],
  cloud: ["remote", "saas"],
  container: ["docker", "pod"],
  plug: ["connect", "adapter", "socket"],
  antenna: ["broadcast", "signal"],
  satellite: ["uplink", "dish"],
  signal: ["reception", "strength", "rssi"],
  usb: ["stick", "thumbdrive", "port"],
  cpu: ["processor", "chip", "compute"],
  memory: ["ram", "dimm"],
  monitor: ["screen", "display", "desktop"],
  laptop: ["computer", "notebook", "workstation"],
  smartphone: ["mobile", "phone", "device"],
  terminal: ["shell", "cli", "console", "bash", "prompt", "command"],
  keyboard: ["input", "typing"],

  // Actions
  pencil: ["edit", "write", "modify"],
  pen: ["write", "edit", "sign"],
  trash: ["delete", "remove", "bin"],
  copy: ["duplicate", "clone"],
  scissors: ["cut"],
  download: ["save", "pull", "fetch", "exfil"],
  upload: ["push", "send", "exfil"],
  share: ["export", "send"],
  link: ["url", "href", "hyperlink", "chain"],
  unlink: ["disconnect", "break"],
  refresh: ["reload", "sync", "retry"],
  rotate: ["turn", "reload"],
  power: ["shutdown", "reboot"],
  play: ["start", "run", "execute"],
  pause: ["hold", "suspend"],
  search: ["find", "magnify", "lookup"],
  zoom: ["magnify", "scale"],
  filter: ["refine", "narrow"],
  funnel: ["filter", "refine"],
  move: ["drag", "reposition"],
  maximize: ["fullscreen", "enlarge", "expand"],
  minimize: ["shrink", "restore", "collapse"],

  // Status and measurement
  check: ["done", "ok", "complete", "tick", "verified"],
  alert: ["warning", "caution", "attention"],
  info: ["note", "about", "details"],
  help: ["question", "faq", "support"],
  bell: ["notify", "notification", "alarm"],
  flag: ["mark", "milestone"],
  bookmark: ["save", "favourite", "favorite"],
  star: ["favourite", "favorite", "important", "rating"],
  clock: ["time", "duration", "schedule"],
  timer: ["countdown", "stopwatch"],
  hourglass: ["wait", "pending", "timeout"],
  calendar: ["date", "schedule"],
  gauge: ["dashboard", "meter", "speed"],
  activity: ["pulse", "heartbeat", "monitor"],
  chart: ["graph", "stats", "analytics", "metrics"],
  trending: ["growth", "increase", "spike"],
  percent: ["rate", "ratio"],

  // People and organisations
  user: ["person", "account", "profile", "operator", "identity"],
  users: ["team", "group", "members"],
  briefcase: ["work", "job", "engagement"],
  building: ["office", "company", "org"],
  factory: ["industry", "plant", "ics"],
  warehouse: ["depot", "storage"],
  landmark: ["bank", "government", "institution"],
  gavel: ["legal", "law", "judge"],
  scale: ["balance", "justice", "weigh"],

  // Tools and code
  code: ["source", "snippet", "script", "dev"],
  braces: ["json", "object"],
  brackets: ["array"],
  regex: ["pattern", "match"],
  variable: ["var", "env"],
  wrench: ["fix", "repair", "config"],
  hammer: ["build", "construct"],
  cog: ["settings", "config", "gear", "preferences"],
  settings: ["config", "gear", "preferences"],
  sliders: ["controls", "tune", "config"],
  puzzle: ["plugin", "extension", "module"],
  package: ["dependency", "module", "npm"],
  box: ["package", "crate"],
  layers: ["stack", "levels"],
  bot: ["agent", "ai", "automation", "robot"],
  brain: ["ai", "intelligence", "model"],
  sparkles: ["ai", "magic", "auto", "new"],
  flask: ["lab", "experiment", "test"],
  microscope: ["research", "analyze", "inspect"],
  test: ["lab", "tube"],

  // Communication and media
  mail: ["email", "smtp", "inbox"],
  message: ["chat", "comment", "dm"],
  phone: ["call", "telephone"],
  mic: ["audio", "voice", "record"],
  volume: ["sound", "audio", "speaker"],
  headphones: ["audio", "listen"],
  video: ["record", "meeting", "call"],
  camera: ["photo", "snapshot"],
  image: ["picture", "photo", "screenshot"],
  film: ["video", "movie"],
  megaphone: ["announce", "broadcast"],

  // Layout and text
  table: ["grid", "spreadsheet", "rows"],
  list: ["items", "bullets"],
  columns: ["layout", "split"],
  rows: ["layout"],
  grid: ["tiles", "layout"],
  panel: ["sidebar", "layout"],
  menu: ["hamburger", "nav"],
  ellipsis: ["dots", "more", "overflow"],
  chevron: ["caret", "expand", "arrow"],
  languages: ["translate", "i18n", "locale"],
  pilcrow: ["paragraph", "formatting"],
  type: ["font", "typography", "text"],
  heading: ["title", "header"],
  align: ["justify"],

  // Other objects
  lightbulb: ["idea", "hint"],
  rocket: ["launch", "ship", "deploy"],
  zap: ["fast", "lightning", "instant"],
  flame: ["fire", "hot", "burn"],
  droplet: ["water", "leak"],
  waves: ["water", "signal"],
  snowflake: ["cold", "freeze"],
  map: ["atlas", "geography"],
  pin: ["location", "marker", "place"],
  compass: ["navigate", "direction"],
  route: ["path", "traceroute", "hop"],
  anchor: ["fixed", "stable"],
  magnet: ["attract", "pull"],
  gift: ["bonus", "reward", "present"],
  trophy: ["win", "achievement"],
  award: ["badge", "prize"],
  graduation: ["learn", "school", "training"],
  coins: ["money", "currency", "cash"],
  banknote: ["money", "cash", "bill"],
  wallet: ["money", "payment"],
  credit: ["card", "payment", "billing"],
  receipt: ["invoice", "bill"],
  stamp: ["approve", "seal"],
  calculator: ["math", "compute"],
  ruler: ["measure", "size"],
}

/** One searchable icon: everything scoring needs, computed once per name. */
export interface IconSearchEntry {
  /** The value stored in `WikiDocument.icon` — `"Server"` or `"si:ubuntu"`. */
  name: string
  /** Lowercased name, or bare slug for a brand icon: what the name tiers match. */
  haystack: string
  tokens: readonly string[]
  /** Hand-written, from the curated catalogs. Empty for uncurated icons. */
  keywords: readonly string[]
  /** Derived from `tokens` via SYNONYMS. */
  synonyms: readonly string[]
  /**
   * lucide's own keywords for this icon, plus each word of a multi-word one, so
   * "command" reaches an icon tagged "command line". Empty until the tag module
   * has loaded, and always empty for brand icons — Simple Icons ships titles
   * and aliases, not tags. See loadIconTags.
   */
  tags: readonly string[]
  /** In one of the curated catalogs — breaks ties, and lifts its keywords. */
  curated: boolean
}

// The tiers, strongest first. Numbers rather than an enum because a
// multi-word query sums one tier per word, so the gaps have to be wide
// enough that a strong match on one word is not outvoted by weak matches on
// the others.
//
// Two orderings here are decisions rather than the obvious thing:
//
// NAME_EXACT leads everything. Typing an icon's name and getting that icon
// first is the one rule worth never breaking, even where it costs an obvious
// intent: "shell" leads with `Shell`, which is a seashell, and puts `Terminal`
// second. Ranking a hand-written keyword above it would fix that case and
// break a commoner one — `FileText` carries the keyword "doc", so a query of
// "doc" would bury `Doc`-named icons under it.
//
// KEYWORD_EXACT then outranks every partial name match, which is what makes
// the curated keywords worth writing: "shell" reaches `Terminal` (a keyword
// someone wrote down) before `SquareTerminal` (a word that merely appears in
// the name). Generated synonyms claim much less and sit below every name tier.
//
// Upstream tags go last of the word tiers, under our own synonyms and under
// every name tier, and that placement is the whole trick to using them. They
// are numerous (13,550 across 1,779 icons) and written for a general audience:
// lucide tags `egg` and `shrimp` with "shell", so a tag tier anywhere higher
// puts breakfast above `SquareTerminal`. Kept last, they add the recall a name
// search cannot have and the noise lands underneath the answers. Where both
// fire, the synonyms above them are the ones tuned to this product, so those
// lead.
const TIER = {
  NAME_EXACT: 1000,
  KEYWORD_EXACT: 900,
  NAME_PREFIX: 800,
  TOKEN_EXACT: 700,
  KEYWORD_PREFIX: 600,
  TOKEN_PREFIX: 500,
  SYNONYM_EXACT: 400,
  TAG_EXACT: 380,
  SYNONYM_PREFIX: 300,
  TAG_PREFIX: 280,
  NAME_SUBSTRING: 200,
  TERM_SUBSTRING: 100,
} as const

// Added once to a matching curated entry, so that between two icons the query
// fits equally well the one someone chose for the catalog wins. Smaller than
// the gap between any two tiers: it settles ties, it does not jump them.
const CURATED_BONUS = 40

/**
 * Splits a raw query into lowercase terms, treating punctuation as a space so
 * a name pasted in any of its spellings still works: `file-code`, `file_code`
 * and `File Code` all arrive as `["file", "code"]`.
 */
export function parseIconQuery(query: string): readonly string[] {
  return query
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter((t) => t.length > 0)
}

function bestTermScore(term: string, entry: IconSearchEntry): number {
  if (entry.haystack === term) return TIER.NAME_EXACT
  if (entry.keywords.some((k) => k === term)) return TIER.KEYWORD_EXACT
  if (entry.haystack.startsWith(term)) return TIER.NAME_PREFIX
  if (entry.tokens.some((t) => t === term)) return TIER.TOKEN_EXACT
  if (entry.keywords.some((k) => k.startsWith(term))) return TIER.KEYWORD_PREFIX
  if (entry.tokens.some((t) => t.startsWith(term))) return TIER.TOKEN_PREFIX
  if (entry.synonyms.some((s) => s === term)) return TIER.SYNONYM_EXACT
  if (entry.tags.some((t) => t === term)) return TIER.TAG_EXACT
  if (entry.synonyms.some((s) => s.startsWith(term))) return TIER.SYNONYM_PREFIX
  if (entry.tags.some((t) => t.startsWith(term))) return TIER.TAG_PREFIX
  if (entry.haystack.includes(term)) return TIER.NAME_SUBSTRING
  if (
    entry.keywords.some((k) => k.includes(term)) ||
    entry.synonyms.some((s) => s.includes(term)) ||
    entry.tags.some((t) => t.includes(term))
  ) {
    return TIER.TERM_SUBSTRING
  }
  return 0
}

/**
 * How well one icon answers a query. `0` means it does not: every term has to
 * land somewhere, so "arrow up" keeps `ArrowUp` and drops `ArrowDown`, which
 * is the behaviour a two-word query is asking for.
 */
export function scoreIcon(
  terms: readonly string[],
  entry: IconSearchEntry,
): number {
  if (terms.length === 0) return 0
  let total = 0
  for (const term of terms) {
    const score = bestTermScore(term, entry)
    if (score === 0) return 0
    total += score
  }
  return total + (entry.curated ? CURATED_BONUS : 0)
}

/**
 * Ranks `entries` against `query` and returns the best `limit` of them.
 *
 * `total` counts every match, not just the returned ones, so the caller can
 * say the list is truncated — without it a capped grid looks like the whole
 * answer and there is no reason to refine the query.
 *
 * Ties break by curated first, then by name length, then alphabetically: with
 * the score equal, the shorter name is the plainer icon (`Server` before
 * `ServerCog`), and the alphabetical last resort keeps the grid from
 * reshuffling between renders on nothing.
 */
export function rankIcons(
  query: string,
  entries: readonly IconSearchEntry[],
  limit: number,
): { names: readonly string[]; total: number } {
  const terms = parseIconQuery(query)
  if (terms.length === 0) return { names: [], total: 0 }

  const hits: { entry: IconSearchEntry; score: number }[] = []
  for (const entry of entries) {
    const score = scoreIcon(terms, entry)
    if (score > 0) hits.push({ entry, score })
  }

  hits.sort((a, b) => {
    if (a.score !== b.score) return b.score - a.score
    if (a.entry.curated !== b.entry.curated) return a.entry.curated ? -1 : 1
    if (a.entry.haystack.length !== b.entry.haystack.length) {
      return a.entry.haystack.length - b.entry.haystack.length
    }
    return a.entry.haystack.localeCompare(b.entry.haystack)
  })

  return {
    names: hits.slice(0, limit).map((h) => h.entry.name),
    total: hits.length,
  }
}

// lucide's own tags, once the generated module has arrived. See loadIconTags.
let loadedTags: Readonly<Record<string, readonly string[]>> = {}

/**
 * The tag terms one icon matches on: each tag, plus the words of a multi-word
 * tag, so `Terminal` (tagged "command line") answers to "command" as well as
 * to the phrase.
 */
function tagTerms(name: string): readonly string[] {
  const tags = loadedTags[name]
  if (!tags) return []
  const terms = new Set<string>()
  for (const tag of tags) {
    terms.add(tag)
    if (tag.includes(" ")) for (const word of tag.split(" ")) terms.add(word)
  }
  return [...terms]
}

/** Builds one entry, deriving tokens, synonyms and tag terms from the name. */
export function iconSearchEntry(
  name: string,
  keywords: readonly string[],
  curated: boolean,
): IconSearchEntry {
  const tokens = tokenizeIconName(name)
  const synonyms: string[] = []
  for (const token of tokens) {
    const extra = SYNONYMS[token]
    if (extra) synonyms.push(...extra)
  }
  return {
    name,
    haystack: simpleIconSlug(name) || name.toLowerCase(),
    tokens,
    keywords,
    synonyms,
    tags: tagTerms(name),
    curated,
  }
}

let index: readonly IconSearchEntry[] | null = null

/**
 * Every icon in one array: the curated lucide catalog, the rest of lucide, the
 * curated brand catalog, the rest of Simple Icons — one entry per name, the
 * curated pass first so its keywords are the ones that survive.
 *
 * Built on first use rather than at import — tokenising five and a half
 * thousand names is a few milliseconds that every page not showing a picker
 * would otherwise pay — and rebuilt once, when the tags arrive. The returned
 * array is stable between those two points, which is what lets the picker
 * subscribe to it through `useSyncExternalStore`.
 */
export function iconSearchIndex(): readonly IconSearchEntry[] {
  if (index) return index

  const entries: IconSearchEntry[] = []

  for (const group of ICON_CATALOG) {
    for (const entry of group.icons) {
      entries.push(iconSearchEntry(entry.name, entry.keywords, true))
    }
  }
  for (const name of ALL_LUCIDE_NAMES.keys()) {
    if (ICON_LOOKUP[name]) continue
    entries.push(iconSearchEntry(name, [], false))
  }
  for (const group of SIMPLE_ICON_CATALOG) {
    for (const entry of group.icons) {
      entries.push(
        iconSearchEntry(toSimpleIconName(entry.slug), entry.keywords, true),
      )
    }
  }
  for (const slug of allSimpleIconSlugs()) {
    if (CURATED_SIMPLE_SLUGS.has(slug)) continue
    entries.push(iconSearchEntry(toSimpleIconName(slug), [], false))
  }

  index = entries
  return index
}

/** Ranked search across both libraries. The picker's whole search path. */
export function searchIcons(
  query: string,
  limit: number,
): { names: readonly string[]; total: number } {
  return rankIcons(query, iconSearchIndex(), limit)
}

// --- lucide's own tags -----------------------------------------------------
//
// 160 KB of metadata for 1,779 of the 2,038 icons — the recall that separates
// this picker's search from the one on lucide.dev, and far too much to put in
// the chunk that renders a tree row. So it is a dynamic import: its own chunk,
// fetched when a picker opens, while search works the whole time on names,
// keywords and synonyms and simply gets better when it lands.
//
// Nothing here throws. A chunk that fails to load costs the tag tiers and
// nothing else, which is a worse search rather than a broken one.

const listeners = new Set<() => void>()
let tagsPromise: Promise<void> | null = null

/**
 * Fetches the tag module, once per session, and rebuilds the index with it.
 * Safe to call on every picker open; the second call is the first one's
 * promise.
 */
export function loadIconTags(): Promise<void> {
  tagsPromise ??= import("@/components/wiki/lucide-tags.generated")
    .then((module) => {
      loadedTags = module.default
      // Entries hold their tag terms, so the memoized index predates them.
      index = null
      for (const listener of listeners) listener()
    })
    .catch(() => {
      // Leave loadedTags empty: every tier below TAG_EXACT still applies.
    })
  return tagsPromise
}

/** True once the tags are in the index. `useSyncExternalStore`'s snapshot. */
export function iconTagsLoaded(): boolean {
  return Object.keys(loadedTags).length > 0
}

/** Subscribes to the one transition this store has — tags arriving. */
export function subscribeIconTags(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

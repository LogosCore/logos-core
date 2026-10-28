#!/usr/bin/env node
// Regenerates public/wordlists/: the word lists the value generator draws
// its words from (src/lib/word-generator.ts; the "Generate" helper of string
// fields with format "path").
//
// Every list is cleaned the same way: lowercase words of 3 to 10 letters
// a–z, each once, sorted, and without offensive words, which are dropped
// against two blocklists used here only and never shipped. A word list loses
// the proper nouns it writes with a capital ("Aristotelian"); a list of
// names or places is lowercased instead.
//
// The sources are fetched and the output is committed, so nothing but this
// script, run by hand when a list should change, needs network.
//
//   npm run gen:wordlists
//
// public/wordlists/index.json lists the lists; NOTICE.md credits every source
// with its terms.

import { mkdirSync, rmSync, writeFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = join(dirname(fileURLToPath(import.meta.url)), "..")
const OUT = join(root, "public/wordlists")

const RAW = "https://raw.githubusercontent.com"
const SCOWL = "https://sources.debian.org/data/main/s/scowl/2020.12.07-2"
const CORPORA = `${RAW}/dariusk/corpora/master/data/words`
const UNG = `${RAW}/andreasonny83/unique-names-generator/main`
const IMSKY = "imsky/wordlists"

async function text(url) {
  const res = await fetch(url, { headers: { "User-Agent": "logos-generate-wordlists" } })
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`)
  return res.text()
}
const json = async (url) => JSON.parse(await text(url))
const lines = (s) =>
  s
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean)

/** Every .txt under a directory of a GitHub repository, its subdirectories' too, by path. */
async function githubTexts(repo, dir) {
  const out = {}
  for (const e of await json(`https://api.github.com/repos/${repo}/contents/${dir}`)) {
    if (e.type === "dir") Object.assign(out, await githubTexts(repo, e.path))
    else if (e.name.endsWith(".txt")) out[e.path] = lines(await text(e.download_url))
  }
  return out
}

/** The quoted strings of a TypeScript dictionary: export default ['a', "b", …]. */
const quoted = (ts) => [...ts.matchAll(/(['"])((?:(?!\1)[^\\]|\\.)*)\1/g)].map((m) => m[2])

console.log("fetching the sources…")
const [scowl10, scowlCopyright, bip39, effShort, effLarge] = await Promise.all([
  text(`${SCOWL}/final/english-words.10`).then(lines),
  text(`${SCOWL}/Copyright`),
  text(`${RAW}/bitcoin/bips/master/bip-0039/english.txt`).then(lines),
  // "1111\tacid": the dice roll, then the word.
  text("https://www.eff.org/files/2016/09/08/eff_short_wordlist_1.txt").then((s) => lines(s).map((l) => l.split(/\s+/)[1])),
  text("https://www.eff.org/files/2016/07/18/eff_large_wordlist.txt").then((s) => lines(s).map((l) => l.split(/\s+/)[1])),
])
const corpora = {
  common: (await json(`${CORPORA}/common.json`)).commonWords,
  adjs: (await json(`${CORPORA}/adjs.json`)).adjs,
  nouns: (await json(`${CORPORA}/nouns.json`)).nouns,
  verbs: (await json(`${CORPORA}/verbs.json`)).verbs.map((v) => v.present),
}
const ung = {}
for (const d of ["adjectives", "animals", "colors", "countries", "names"]) {
  ung[d] = quoted(await text(`${UNG}/src/dictionaries/${d}.ts`))
}
const ungLicense = await text(`${UNG}/LICENSE`)
const imsky = {
  ...(await githubTexts(IMSKY, "adjectives")),
  ...(await githubTexts(IMSKY, "verbs")),
  ...(await githubTexts(IMSKY, "nouns")),
  // Names of people and places; not codenames or companies, which are brands.
  ...(await githubTexts(IMSKY, "names/people")),
  ...(await githubTexts(IMSKY, "names/surnames")),
  ...(await githubTexts(IMSKY, "names/robots")),
  ...(await githubTexts(IMSKY, "names/cities")),
  ...(await githubTexts(IMSKY, "names/states")),
  ...(await githubTexts(IMSKY, "names/streets")),
}
const imskyLicense = await text(`${RAW}/${IMSKY}/master/LICENSE`)
const imskyUnder = (prefix) =>
  Object.entries(imsky)
    .filter(([path]) => path.startsWith(prefix))
    .flatMap(([, words]) => words)

// Used only to leave words out, never shipped: Corpora's expletives (CC0) and
// LDNOOBW's English list.
const blocked = new Set(
  [
    ...(await json(`${CORPORA}/expletives.json`)).expletives,
    ...lines(await text(`${RAW}/LDNOOBW/List-of-Dirty-Naughty-Obscene-and-Otherwise-Bad-Words/master/en`)),
  ].map((w) => w.toLowerCase()),
)

const WORD = /^[a-z]{3,10}$/

/** Words fit for the generator: lowercase a–z, 3 to 10 letters, none blocked, each once, sorted. */
function clean(words, { names = false } = {}) {
  const out = new Set()
  for (const raw of words) {
    const w = names ? raw.toLowerCase() : raw
    if (WORD.test(w) && !blocked.has(w)) out.add(w)
  }
  return [...out].sort()
}

// kind tells the generator's "adjective + noun" pattern where to take each
// word from: "word" lists take no part in it.
const lists = [
  { id: "common", name: "Common words", kind: "word", sources: ["scowl", "corpora"], words: clean([...scowl10, ...corpora.common]) },
  { id: "short", name: "Short words", kind: "word", sources: ["eff"], words: clean(effShort) },
  { id: "long", name: "Long word list", kind: "word", sources: ["eff"], words: clean(effLarge) },
  { id: "bip39", name: "BIP-39 words", kind: "word", sources: ["bip39"], words: clean(bip39) },
  {
    id: "adjectives",
    name: "Adjectives",
    kind: "adjective",
    sources: ["corpora", "imsky", "ung"],
    words: clean([...corpora.adjs, ...imskyUnder("adjectives/"), ...ung.adjectives]),
  },
  { id: "nouns", name: "Nouns", kind: "noun", sources: ["corpora", "imsky"], words: clean([...corpora.nouns, ...imskyUnder("nouns/")]) },
  { id: "verbs", name: "Verbs", kind: "verb", sources: ["corpora", "imsky"], words: clean([...corpora.verbs, ...imskyUnder("verbs/")]) },
  { id: "animals", name: "Animals", kind: "noun", sources: ["ung"], words: clean(ung.animals, { names: true }) },
  {
    id: "colors",
    name: "Colors",
    kind: "adjective",
    sources: ["ung", "imsky"],
    words: clean([...ung.colors, ...imskyUnder("adjectives/colors")], { names: true }),
  },
  {
    id: "places",
    name: "Places",
    kind: "noun",
    sources: ["ung", "imsky"],
    words: clean([...ung.countries, ...imskyUnder("names/cities"), ...imskyUnder("names/states"), ...imskyUnder("names/streets")], {
      names: true,
    }),
  },
  {
    id: "names",
    name: "Names",
    kind: "noun",
    sources: ["ung", "imsky"],
    words: clean([...ung.names, ...imskyUnder("names/people"), ...imskyUnder("names/surnames"), ...imskyUnder("names/robots")], {
      names: true,
    }),
  },
]

// imsky's nouns by theme, each a list of its own.
const ACRONYMS = { "3d": "3D", vcs: "VCS" }
const themeName = (stem) => {
  const words = stem.split("_").map((w) => ACRONYMS[w] ?? w)
  return words.map((w, i) => (i === 0 ? w[0].toUpperCase() + w.slice(1) : w)).join(" ")
}
const themes = Object.entries(imsky)
  .filter(([path]) => path.startsWith("nouns/"))
  .map(([path, words]) => {
    const stem = path.slice("nouns/".length, -".txt".length)
    return { id: `theme-${stem.replaceAll("_", "-")}`, name: themeName(stem), kind: "noun", theme: true, sources: ["imsky"], words: clean(words) }
  })
  .sort((a, b) => a.name.localeCompare(b.name))

// A list too short to draw from is left out.
const MIN = 8
const shipped = [...lists, ...themes].filter((l) => {
  if (l.words.length >= MIN) return true
  console.log(`  left out ${l.id}: ${l.words.length} words`)
  return false
})

const shippedThemes = shipped.filter((l) => l.theme)

const SOURCES = {
  scowl: {
    name: "SCOWL (Spell Checker Oriented Word Lists), size 10",
    url: "http://wordlist.aspell.net/",
    license: "SCOWL's permission notice: any use, the copyright notice kept",
  },
  corpora: { name: "Corpora by Darius Kazemi and contributors", url: "https://github.com/dariusk/corpora", license: "CC0 1.0" },
  imsky: { name: "wordlists by Ivan Malopinsky", url: "https://github.com/imsky/wordlists", license: "MIT" },
  ung: { name: "unique-names-generator by Andrea SonnY", url: "https://github.com/andreasonny83/unique-names-generator", license: "MIT" },
  bip39: { name: "BIP-0039 English wordlist", url: "https://github.com/bitcoin/bips/blob/master/bip-0039.mediawiki", license: "MIT" },
  eff: { name: "EFF's Wordlists for Random Passphrases", url: "https://www.eff.org/dice", license: "CC BY 3.0 US" },
}

rmSync(OUT, { recursive: true, force: true })
mkdirSync(OUT, { recursive: true })
for (const l of shipped) writeFileSync(join(OUT, `${l.id}.txt`), `${l.words.join("\n")}\n`)
writeFileSync(
  join(OUT, "index.json"),
  `${JSON.stringify(
    {
      lists: shipped.map(({ id, name, kind, theme, sources, words }) => ({ id, name, kind, ...(theme ? { theme } : {}), count: words.length, sources })),
      sources: SOURCES,
    },
    null,
    2,
  )}\n`,
)

const credits = (ids) => ids.map((id) => SOURCES[id].name).join("; ")
writeFileSync(
  join(OUT, "NOTICE.md"),
  `# Word lists

The value generator (the "Generate" helper of fields that hold a path) picks
its words from these lists. \`scripts/generate-wordlists.mjs\` builds them from
the sources below: each list holds lowercase words of 3 to 10 letters a–z,
each once, without offensive words; dice numbers and the words that do not
fit are left out of the sources' lists.

| List | Words | From |
|---|---|---|
${shipped
  .filter((l) => !l.theme)
  .map((l) => `| ${l.name} | ${l.words.length} | ${credits(l.sources)} |`)
  .join("\n")}
| ${shippedThemes.length} themes (Coding, Food, …) | ${shippedThemes.reduce((n, l) => n + l.words.length, 0)} | ${credits(["imsky"])} |

## SCOWL

SCOWL (Spell Checker Oriented Word Lists) 2020.12.07, size 10, by Kevin
Atkinson: ${SOURCES.scowl.url}. Its copyright and permission notice:

\`\`\`
${scowlCopyright.trim()}
\`\`\`

## Corpora

Corpora, by Darius Kazemi and contributors: ${SOURCES.corpora.url}. Its data
is dedicated to the public domain under CC0 1.0
(https://creativecommons.org/publicdomain/zero/1.0/).

## wordlists

wordlists, by Ivan Malopinsky: ${SOURCES.imsky.url}.

\`\`\`
${imskyLicense.trim()}
\`\`\`

## unique-names-generator

unique-names-generator, by Andrea SonnY: ${SOURCES.ung.url}.

\`\`\`
${ungLicense.trim()}
\`\`\`

## BIP-0039

The English wordlist of BIP-0039, "Mnemonic code for generating
deterministic keys", by Marek Palatinus, Pavol Rusnak, Aaron Voisine and Sean
Bowe: ${SOURCES.bip39.url}. The BIP states: "License: MIT".

## EFF's Wordlists for Random Passphrases

EFF's Long Wordlist and Short Wordlist #1, by Joseph Bonneau and the
Electronic Frontier Foundation: ${SOURCES.eff.url}. Licensed under the
Creative Commons Attribution 3.0 United States License
(https://creativecommons.org/licenses/by/3.0/us/). Changed: the dice numbers
are left out, and so are the words that do not fit the rule above.
`,
)
console.log(`wrote ${shipped.length} lists to public/wordlists (${shipped.reduce((n, l) => n + l.words.length, 0)} words)`)

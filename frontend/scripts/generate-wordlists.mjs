#!/usr/bin/env node
// Regenerates public/wordlists/: the word lists the value generator draws
// its words from (src/lib/word-generator.ts; the "Generate" helper of string
// fields with format "path").
//
// Three lists: Common words (SCOWL size 10 with Corpora's common words),
// and EFF's short and long passphrase lists. Every list is cleaned the same
// way: lowercase words of 3 to 10 letters a–z, each once, sorted, and
// without offensive words, which are dropped against two blocklists used
// here only and never shipped; a word written with a capital, a proper
// noun, is left out.
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

console.log("fetching the sources…")
const [scowl10, scowlCopyright, effShort, effLarge] = await Promise.all([
  text(`${SCOWL}/final/english-words.10`).then(lines),
  text(`${SCOWL}/Copyright`),
  // "1111\tacid": the dice roll, then the word.
  text("https://www.eff.org/files/2016/09/08/eff_short_wordlist_1.txt").then((s) => lines(s).map((l) => l.split(/\s+/)[1])),
  text("https://www.eff.org/files/2016/07/18/eff_large_wordlist.txt").then((s) => lines(s).map((l) => l.split(/\s+/)[1])),
])
const common = (await json(`${CORPORA}/common.json`)).commonWords

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
function clean(words) {
  return [...new Set(words.filter((w) => WORD.test(w) && !blocked.has(w)))].sort()
}

const lists = [
  { id: "common", name: "Common words", sources: ["scowl", "corpora"], words: clean([...scowl10, ...common]) },
  { id: "short", name: "Short words", sources: ["eff"], words: clean(effShort) },
  { id: "long", name: "Long word list", sources: ["eff"], words: clean(effLarge) },
]

const SOURCES = {
  scowl: {
    name: "SCOWL (Spell Checker Oriented Word Lists), size 10",
    url: "http://wordlist.aspell.net/",
    license: "SCOWL's permission notice: any use, the copyright notice kept",
  },
  corpora: { name: "Corpora by Darius Kazemi and contributors", url: "https://github.com/dariusk/corpora", license: "CC0 1.0" },
  eff: { name: "EFF's Wordlists for Random Passphrases", url: "https://www.eff.org/dice", license: "CC BY 3.0 US" },
}

rmSync(OUT, { recursive: true, force: true })
mkdirSync(OUT, { recursive: true })
for (const l of lists) writeFileSync(join(OUT, `${l.id}.txt`), `${l.words.join("\n")}\n`)
writeFileSync(
  join(OUT, "index.json"),
  `${JSON.stringify(
    {
      lists: lists.map(({ id, name, sources, words }) => ({ id, name, count: words.length, sources })),
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
${lists.map((l) => `| ${l.name} | ${l.words.length} | ${credits(l.sources)} |`).join("\n")}

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

## EFF's Wordlists for Random Passphrases

EFF's Long Wordlist and Short Wordlist #1, by Joseph Bonneau and the
Electronic Frontier Foundation: ${SOURCES.eff.url}. Licensed under the
Creative Commons Attribution 3.0 United States License
(https://creativecommons.org/licenses/by/3.0/us/). Changed: the dice numbers
are left out, and so are the words that do not fit the rule above.
`,
)
console.log(`wrote ${lists.length} lists to public/wordlists (${lists.reduce((n, l) => n + l.words.length, 0)} words)`)

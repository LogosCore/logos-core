#!/usr/bin/env node
// Regenerates public/wordlists/: the word lists the value generator draws
// its words from (src/lib/word-generator.ts; the "Generate" helper of string
// fields with format "path").
//
// Four lists: Common words (SCOWL size 10 with Corpora's common words),
// EFF's short and long passphrase lists, and Directory names. The first
// three hold lowercase words of 3 to 10 letters a–z (a word written with a
// capital, a proper noun, is left out). Directory names are merged from
// web content discovery lists the operators keep in a folder of their own,
// cut to names fit for a path segment (directoryName). Every list holds
// each word once, sorted, without offensive words, which are dropped
// against two blocklists used here only and never shipped.
//
// The sources are fetched (or read from that folder) and the output is
// committed, so nothing but this script, run by hand when a list should
// change, needs them.
//
//   npm run gen:wordlists                               # Directory names kept as committed
//   npm run gen:wordlists -- --directories ~/wordlists  # Directory names rebuilt from a folder
//
// public/wordlists/index.json lists the lists; NOTICE.md credits every source
// with its terms.

import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = join(dirname(fileURLToPath(import.meta.url)), "..")
const OUT = join(root, "public/wordlists")

const RAW = "https://raw.githubusercontent.com"
const SCOWL = "https://sources.debian.org/data/main/s/scowl/2020.12.07-2"
const CORPORA = `${RAW}/dariusk/corpora/master/data/words`

// The folder Directory names are merged from: every file in it, one name a
// line, "#" lines skipped. Without it, the committed list is kept.
const at = process.argv.indexOf("--directories")
const DIRECTORIES = at >= 0 ? process.argv[at + 1] : null
if (at >= 0 && !DIRECTORIES) throw new Error("--directories needs a folder")

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

const DIRECTORY = /^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$/

/**
 * A directory name fit for a path segment, or null: lowercased, slashes
 * around it trimmed; from a letter on, letters and digits in parts joined
 * by one - or _; 3 to 20 characters with at most 4 digits, so no ids or
 * hashes; no part of it blocked. File names, traversal tricks and symbols
 * do not pass.
 */
function directoryName(raw) {
  const w = raw.trim().replace(/^\/+|\/+$/g, "").toLowerCase()
  if (w.length < 3 || w.length > 20 || !DIRECTORY.test(w)) return null
  if ((w.match(/[0-9]/g) ?? []).length > 4) return null
  if (blocked.has(w) || w.split(/[-_]/).some((part) => blocked.has(part))) return null
  return w
}

/** Directory names: merged from the folder's files, or the committed list kept. */
function directoryNames() {
  if (DIRECTORIES) {
    const names = new Set()
    for (const file of readdirSync(DIRECTORIES).sort()) {
      const path = join(DIRECTORIES, file)
      if (!statSync(path).isFile()) continue
      let kept = 0
      for (const line of readFileSync(path, "utf8").split(/\r?\n/)) {
        const name = line.startsWith("#") ? null : directoryName(line)
        if (name) {
          names.add(name)
          kept++
        }
      }
      console.log(`  ${file}: ${kept} names`)
    }
    return [...names].sort()
  }
  const committed = join(OUT, "directory.txt")
  if (!existsSync(committed)) return []
  console.log("  Directory names kept as committed (--directories rebuilds them)")
  return lines(readFileSync(committed, "utf8"))
}

const lists = [
  { id: "common", name: "Common words", sources: ["scowl", "corpora"], words: clean([...scowl10, ...common]) },
  { id: "short", name: "Short words", sources: ["eff"], words: clean(effShort) },
  { id: "long", name: "Long word list", sources: ["eff"], words: clean(effLarge) },
  { id: "directory", name: "Directory names", sources: ["dirbuster", "operators"], words: directoryNames() },
].filter((l) => l.words.length > 0)

const SOURCES = {
  scowl: {
    name: "SCOWL (Spell Checker Oriented Word Lists), size 10",
    url: "http://wordlist.aspell.net/",
    license: "SCOWL's permission notice: any use, the copyright notice kept",
  },
  corpora: { name: "Corpora by Darius Kazemi and contributors", url: "https://github.com/dariusk/corpora", license: "CC0 1.0" },
  eff: { name: "EFF's Wordlists for Random Passphrases", url: "https://www.eff.org/dice", license: "CC BY 3.0 US" },
  dirbuster: {
    name: "DirBuster's directory-list-2.3-medium by James Fisher",
    url: "https://github.com/danielmiessler/SecLists/blob/master/Discovery/Web-Content/DirBuster-2007_directory-list-2.3-medium.txt",
    license: "CC BY-SA 3.0",
  },
  operators: { name: "the operators' web path lists", url: "", license: "origin not recorded" },
}

// Written over in place, and stale files removed one by one: a dev server
// watching the folder loses track of its files when it goes whole.
mkdirSync(OUT, { recursive: true })
const written = new Set([...lists.map((l) => `${l.id}.txt`), "index.json", "NOTICE.md"])
for (const file of readdirSync(OUT)) if (!written.has(file)) rmSync(join(OUT, file))
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
the sources below: Common words, Short words and Long word list hold
lowercase words of 3 to 10 letters a–z; Directory names, names fit for a
path segment (see its section). Every list holds each word once, without
offensive words; dice numbers and the words that do not fit are left out of
the sources' lists.

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

## Directory names

Merged from web content discovery lists the operators keep, cut to names
fit for a path segment: lowercase, from a letter on, letters and digits in
parts joined by one \`-\` or \`_\`, 3 to 20 characters with at most 4
digits, no offensive part. File names, ids and symbols are left out.

- \`directory-list-2.3-medium.txt\` and its lowercase version, from DirBuster:
  Copyright 2007 James Fisher, licensed under the Creative Commons
  Attribution-Share Alike 3.0 License
  (https://creativecommons.org/licenses/by-sa/3.0/). Built on it, the
  Directory names list is shared under the same license.
- \`directories.txt\`, \`directories(1).txt\`, \`most_common.txt\` and
  \`web_path_wordlist.txt\`: the operators' own; their origin is not recorded.
`,
)
console.log(`wrote ${lists.length} lists to public/wordlists (${lists.reduce((n, l) => n + l.words.length, 0)} words)`)

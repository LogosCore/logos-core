import type { WordlistCatalog } from "@/lib/word-generator"

// The shipped word lists (public/wordlists, built by
// scripts/generate-wordlists.mjs), fetched when the value generator first
// needs one. Static files: nothing of them is in the bundle.

const BASE = "/wordlists"

/** Where the lists' sources are credited, with their terms. */
export const WORDLIST_CREDITS = `${BASE}/NOTICE.md`

export async function fetchWordlistCatalog(): Promise<WordlistCatalog> {
  const res = await fetch(`${BASE}/index.json`)
  if (!res.ok) throw new Error(`The word lists did not load (HTTP ${res.status})`)
  return (await res.json()) as WordlistCatalog
}

export async function fetchWordlist(id: string): Promise<string[]> {
  const res = await fetch(`${BASE}/${encodeURIComponent(id)}.txt`)
  if (!res.ok) throw new Error(`The word list ${id} did not load (HTTP ${res.status})`)
  return (await res.text()).split("\n").filter(Boolean)
}

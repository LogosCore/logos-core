import { describe, expect, test } from "vitest"
import { ICON_CATALOG } from "@/components/wiki/icon-catalog"
import {
  iconSearchIndex,
  parseIconQuery,
  searchIcons,
  SYNONYMS,
  tokenizeIconName,
} from "@/components/wiki/icon-search"

/** The ranked names for a query, with a generous cap unless one is given. */
const search = (q: string, limit = 40) => searchIcons(q, limit).names

describe("tokenizeIconName", () => {
  test("splits PascalCase into words", () => {
    expect(tokenizeIconName("ServerCog")).toEqual(["server", "cog"])
    expect(tokenizeIconName("CircleUserRound")).toEqual([
      "circle",
      "user",
      "round",
    ])
  })

  test("keeps a leading acronym apart from the word after it", () => {
    // Greedy [A-Z]+ would take "Ar" out of "AArrowDown" and lose the "a".
    expect(tokenizeIconName("AArrowDown")).toEqual(["a", "arrow", "down"])
    expect(tokenizeIconName("QrCode")).toEqual(["qr", "code"])
  })

  test("splits trailing digits off the word", () => {
    // lucide numbers its variants, and "columns3" answers to no query anyone
    // types — the whole reason "columns" has to survive as its own token.
    expect(tokenizeIconName("Columns3")).toEqual(["columns", "3"])
    expect(tokenizeIconName("Volume2")).toEqual(["volume", "2"])
  })

  test("strips the brand prefix and splits the slug", () => {
    expect(tokenizeIconName("si:kalilinux")).toEqual(["kalilinux"])
    expect(tokenizeIconName("si:1password")).toEqual(["1", "password"])
  })
})

describe("parseIconQuery", () => {
  test("treats punctuation as a word break", () => {
    // Someone pasting a name they saw in code should not have to reformat it.
    for (const q of ["file-code", "file_code", "File Code", "file/code"]) {
      expect(parseIconQuery(q)).toEqual(["file", "code"])
    }
  })

  test("is empty for a query with nothing to match on", () => {
    expect(parseIconQuery("   ")).toEqual([])
    expect(parseIconQuery("-")).toEqual([])
  })
})

describe("SYNONYMS", () => {
  test("every key is a real token of some lucide icon", () => {
    // A key that matches no name is dead weight that reads as coverage. This
    // catches a typo, and a lucide rename that drops a word out of the set.
    const tokens = new Set<string>()
    for (const entry of iconSearchIndex()) {
      for (const token of entry.tokens) tokens.add(token)
    }
    const orphans = Object.keys(SYNONYMS).filter((k) => !tokens.has(k))
    expect(orphans).toEqual([])
  })

  test("no synonym merely restates its own key", () => {
    for (const [token, terms] of Object.entries(SYNONYMS)) {
      expect(terms, token).not.toContain(token)
    }
  })
})

describe("ranking", () => {
  test("an exact name wins", () => {
    expect(search("server")[0]).toBe("Server")
    expect(search("hash")[0]).toBe("Hash")
  })

  test("the plainer name leads a shared prefix", () => {
    const names = search("serv")
    expect(names[0]).toBe("Server")
    expect(names.indexOf("Server")).toBeLessThan(names.indexOf("ServerCog"))
  })

  test("a hand-written keyword beats a word that only appears in a name", () => {
    // "shell" is a keyword on the curated Terminal; SquareTerminal merely
    // contains the token "terminal".
    const names = search("shell")
    expect(names.indexOf("Terminal")).toBeLessThan(
      names.indexOf("SquareTerminal"),
    )
  })

  test("an exact name still leads a hand-written keyword", () => {
    // Shell is a seashell and nobody searching "shell" wants it — but the
    // rule that typing a name finds that icon is worth more than this case,
    // and Terminal is the tile next to it. See TIER in icon-search.ts.
    expect(search("shell")[0]).toBe("Shell")
  })

  test("synonyms reach the uncurated set through its name tokens", () => {
    // None of these words appear in the names they have to find, and none of
    // the icons carries a hand-written keyword.
    expect(search("vulnerability")).toContain("Bug")
    expect(search("wireless")).toContain("Wifi")
    expect(search("injection")).toContain("Syringe")
    expect(search("ntlm")).toContain("Hash")
    expect(search("traceroute")).toContain("Route")
  })

  test("one synonym entry serves every name holding its token", () => {
    // The point of keying synonyms on tokens: "console" was written once, for
    // "terminal", and reaches the variants nobody curated.
    const names = search("console")
    expect(names).toContain("Terminal")
    expect(names).toContain("SquareTerminal")
  })

  test("brand logos rank with everything else rather than under it", () => {
    // The old picker put every brand hit below a "More icons" heading, so the
    // one real answer for this query needed a scroll past irrelevant tiles.
    expect(search("ubuntu")[0]).toBe("si:ubuntu")
    expect(search("kali")[0]).toBe("si:kalilinux")
  })

  test("a curated brand keyword finds its logo", () => {
    expect(search("rhel")).toContain("si:redhat")
  })

  test("every word has to match", () => {
    const names = search("arrow up")
    expect(names).toContain("ArrowUp")
    expect(names).not.toContain("ArrowDown")
  })

  test("a query matching nothing returns nothing", () => {
    const result = searchIcons("zzqqnotanicon", 40)
    expect(result.names).toEqual([])
    expect(result.total).toBe(0)
  })

  test("an empty query matches nothing rather than everything", () => {
    // The picker browses the curated groups instead of searching, so this path
    // returning the whole index would be 5,500 lazy chunks on an empty input.
    expect(searchIcons("", 40).total).toBe(0)
    expect(searchIcons("  ", 40).total).toBe(0)
  })

  test("total counts the matches beyond the cap", () => {
    // What the "top N of M" hint is built from; without it a capped grid reads
    // as the whole answer.
    const result = searchIcons("a", 10)
    expect(result.names).toHaveLength(10)
    expect(result.total).toBeGreaterThan(10)
  })

  test("results are unique — one entry per stored name", () => {
    const names = search("file", 96)
    expect(new Set(names).size).toBe(names.length)
  })

  test("every curated icon is the first hit for its own name", () => {
    // The catalog is what the picker offers first while browsing, so a curated
    // icon losing its own name to something out of the uncurated 1,900 would
    // be a search actively working against the catalog.
    for (const group of ICON_CATALOG) {
      for (const entry of group.icons) {
        expect(search(entry.name, 1)[0], entry.name).toBe(entry.name)
      }
    }
  })

  test("every curated keyword finds its own icon", () => {
    // Hand-written keywords are the strongest signal in the ranking; one that
    // does not surface its icon is a keyword that silently does nothing.
    for (const group of ICON_CATALOG) {
      for (const entry of group.icons) {
        for (const keyword of entry.keywords) {
          expect(search(keyword, 96), `${entry.name} / ${keyword}`).toContain(
            entry.name,
          )
        }
      }
    }
  })
})

describe("iconSearchIndex", () => {
  test("holds both libraries, curated and uncurated", () => {
    const index = iconSearchIndex()
    expect(index.length).toBeGreaterThan(5000)
    const byName = new Map(index.map((e) => [e.name, e]))
    expect(byName.get("Terminal")?.curated).toBe(true)
    expect(byName.get("SquareTerminal")?.curated).toBe(false)
    expect(byName.get("si:ubuntu")?.curated).toBe(true)
  })

  test("names appear once, with the curated keywords kept", () => {
    const index = iconSearchIndex()
    expect(new Set(index.map((e) => e.name)).size).toBe(index.length)
    const terminal = index.find((e) => e.name === "Terminal")
    expect(terminal?.keywords).toContain("shell")
  })

  test("is memoized", () => {
    expect(iconSearchIndex()).toBe(iconSearchIndex())
  })
})

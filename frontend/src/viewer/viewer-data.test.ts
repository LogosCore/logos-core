import { describe, expect, it } from "vitest"
import { ancestorsOf, hashForPage, indexTree, matchRanges, pageIdFromHash, queryWords, search, type ExportPage } from "./viewer-data"

const pages: ExportPage[] = [
  { id: "net", title: "Network", kind: "document", text: "Dual-homed edge. Gateway in-bgp01.\n" },
  { id: "peer", parentId: "net", title: "Peering", kind: "document", text: "IX-1 session, ASN 64512\n" },
  { id: "deep", parentId: "peer", title: "Session log", kind: "document", text: "flap at 02:00\n" },
  { id: "draw", title: "Diagram", kind: "drawing", text: "Edge router\n" },
  // A parent outside the export: the page is a root here.
  { id: "orphan", parentId: "not-exported", title: "Loose page", kind: "document" },
]

describe("indexTree", () => {
  it("keeps the export's order and treats a page whose parent is not exported as a root", () => {
    const index = indexTree(pages)
    expect(index.roots.map((p) => p.id)).toEqual(["net", "draw", "orphan"])
    expect(index.children.get("net")?.map((p) => p.id)).toEqual(["peer"])
    expect(index.children.get("peer")?.map((p) => p.id)).toEqual(["deep"])
  })

  it("lists ancestors root first", () => {
    const index = indexTree(pages)
    expect(ancestorsOf(index, "deep").map((p) => p.id)).toEqual(["net", "peer"])
    expect(ancestorsOf(index, "net")).toEqual([])
  })

  it("does not loop on a cycle in bad data", () => {
    const index = indexTree([
      { id: "a", parentId: "b", title: "A", kind: "document" },
      { id: "b", parentId: "a", title: "B", kind: "document" },
    ])
    expect(ancestorsOf(index, "a").length).toBeLessThanOrEqual(2)
  })
})

describe("search", () => {
  it("matches titles and bodies, titles first", () => {
    const hits = search(pages, "edge")
    expect(hits.map((h) => h.page.id)).toEqual(["net", "draw"])
    expect(hits[0].titleMatch).toBe(false)
    expect(search(pages, "peering")[0]).toMatchObject({ titleMatch: true, page: { id: "peer" } })
  })

  it("requires every word, in any field, ignoring case", () => {
    expect(search(pages, "asn ix-1").map((h) => h.page.id)).toEqual(["peer"])
    expect(search(pages, "asn router")).toEqual([])
  })

  it("returns a snippet around the first match", () => {
    const [hit] = search(pages, "64512")
    expect(hit.snippet).toContain("64512")
  })

  it("ignores an empty query", () => {
    expect(search(pages, "   ")).toEqual([])
  })
})

describe("hash routes", () => {
  it("round-trip page ids", () => {
    expect(pageIdFromHash(hashForPage("8f63e506-dc8c"))).toBe("8f63e506-dc8c")
    expect(pageIdFromHash("#/elsewhere")).toBeNull()
    expect(pageIdFromHash("")).toBeNull()
  })
})

describe("matchRanges", () => {
  it("finds every occurrence of every word, ignoring case", () => {
    expect(matchRanges("Edge router, edge switch", ["edge"])).toEqual([[0, 4], [13, 17]])
    expect(matchRanges("ASN 64512 on IX-1", queryWords("ix-1 asn"))).toEqual([[0, 3], [13, 17]])
  })

  it("merges overlapping and touching matches", () => {
    expect(matchRanges("edge-homed", ["edge", "dge-ho"])).toEqual([[0, 7]])
    expect(matchRanges("abcd", ["ab", "cd"])).toEqual([[0, 4]])
  })

  it("finds nothing for no words", () => {
    expect(matchRanges("anything", [])).toEqual([])
  })
})

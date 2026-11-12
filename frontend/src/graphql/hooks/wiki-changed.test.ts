import { describe, expect, test } from "vitest"
import { QueryClient, type QueryKey } from "@tanstack/react-query"
import type { WikiDocumentChildrenQuery, WikiDocumentTreeFieldsFragment } from "@/graphql/gql/graphql"
import { applyWikiDocumentChanged, childrenBucketsHolding, wikiKeys } from "./wiki"

const OP = "op-1"

const row = (id: string, parentDocumentId: string | null, childCount = 0) =>
  ({ id, operationId: OP, parentDocumentId, childCount, sortOrder: id }) as WikiDocumentTreeFieldsFragment

// The sidebar's buckets: the roots, and the children of "a" (a page under the
// root "a", the page "p", a leaf so far, and its sibling "s").
function seeded() {
  const qc = new QueryClient()
  const put = (parent: string | null, rows: WikiDocumentTreeFieldsFragment[]) =>
    qc.setQueryData<WikiDocumentChildrenQuery>(wikiKeys.children(OP, parent), { wikiDocumentChildren: rows })
  put(null, [row("a", null, 2)])
  put("a", [row("p", "a"), row("s", "a")])
  return qc
}

const stale = (qc: QueryClient, key: QueryKey) => qc.getQueryState(key)?.isInvalidated ?? false

describe("a wikiDocumentChanged event", () => {
  // The bug: an agent added a page's first child; the page's row (in its
  // parent's bucket) kept childCount 0, so no caret until a reload.
  test("a child created under a page refreshes the bucket holding the page's row", () => {
    const qc = seeded()
    applyWikiDocumentChanged(qc, OP, { action: "CREATED", documentId: "c", operationId: OP, parentDocumentId: "p" })
    expect(stale(qc, wikiKeys.children(OP, "a"))).toBe(true) // p's row: its childCount
    expect(stale(qc, wikiKeys.children(OP, null))).toBe(false) // a's row did not change
  })

  test("a page moved away refreshes the rows of both parents", () => {
    const qc = seeded()
    applyWikiDocumentChanged(qc, OP, {
      action: "UPDATED",
      documentId: "c",
      operationId: OP,
      parentDocumentId: "a",
      previousParentDocumentId: "p",
    })
    expect(stale(qc, wikiKeys.children(OP, "a"))).toBe(true) // p's row, and a's children
    expect(stale(qc, wikiKeys.children(OP, null))).toBe(true) // a's row: a gained a child
  })

  test("a root page refreshes only the roots", () => {
    const qc = seeded()
    applyWikiDocumentChanged(qc, OP, { action: "CREATED", documentId: "r", operationId: OP, parentDocumentId: null })
    expect(stale(qc, wikiKeys.children(OP, null))).toBe(true)
    expect(stale(qc, wikiKeys.children(OP, "a"))).toBe(false)
  })
})

describe("childrenBucketsHolding", () => {
  test("finds the loaded buckets with the rows, in this operation only", () => {
    const qc = seeded()
    qc.setQueryData<WikiDocumentChildrenQuery>(wikiKeys.children("op-2", null), {
      wikiDocumentChildren: [{ ...row("p", null), operationId: "op-2" }],
    })
    expect(childrenBucketsHolding(qc, OP, ["p"])).toEqual([wikiKeys.children(OP, "a")])
    expect(childrenBucketsHolding(qc, OP, ["nowhere"])).toEqual([])
    expect(childrenBucketsHolding(qc, OP, [])).toEqual([])
  })
})

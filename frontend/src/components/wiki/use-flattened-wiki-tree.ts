import { useEffect, useLayoutEffect, useMemo } from "react"
import { useQueries } from "@tanstack/react-query"
import {
  useWikiDocumentChildren,
  wikiChildrenQueryOptions,
} from "@/graphql/hooks/wiki"
import { rowToTreeNode, sortByOrder } from "@/components/wiki/wiki-tree-helpers"
import { settleRestoredIds, useWikiStore } from "@/stores/wiki"
import type { TreeNode } from "@/components/wiki/wiki-tree-sidebar"
import type { WikiDocumentChildrenQuery } from "@/graphql/gql/graphql"

// One entry in the flattened, windowable tree. A `node` row is a real document;
// a `skeleton` row is the placeholder shown under an expanded branch whose
// children query is still in flight (mirrors the old per-branch loading state).
export type FlatRow =
  | { kind: "node"; key: string; node: TreeNode; depth: number }
  | { kind: "skeleton"; key: string; depth: number }

export interface FlattenedWikiTree {
  rows: FlatRow[]
  rootsLoading: boolean
}

const NONE_EXPANDED: ReadonlySet<string> = new Set()

/**
 * Flatten the *currently visible* wiki tree into a single ordered array so the
 * sidebar can virtualize it (render only the rows in the viewport).
 *
 * Why this exists: the tree is lazily loaded per branch — each parent's direct
 * children live under their own React Query key. To virtualize we need one flat
 * list, which means lifting children fetching out of the recursive row and into
 * this controller.
 *
 * Fetching strategy: we subscribe (via `useQueries`) to the children of every
 * id in `expandedNodes`. That keeps the controller reactive — when any branch
 * resolves, the list re-flattens. The chevron only appears on nodes with
 * children, so ids enter `expandedNodes` as real parents; one that has since
 * lost its children (or been deleted) resolves to an empty slice, never
 * surfaces in the walk, and is dropped on the next load (see settleExpanded
 * below). Requesting a few children-of-collapsed-ancestor branches is the
 * only redundancy, and those were already fetched under the previous
 * per-node scheme.
 *
 * Convergence: the walk only descends through a parent once its children are in
 * the `useQueries` results. Deeper levels become known as their parent resolves,
 * so a freshly-revealed deep path fills in over O(depth) renders.
 */
export function useFlattenedWikiTree(operationId: string): FlattenedWikiTree {
  // The store holds one operation's expanded set at a time. Swap in this
  // one's before the child queries subscribe; until the swap lands, the set
  // on hand is another operation's and reads as empty, rather than asking
  // this operation for the children of those ids.
  const showOperationTree = useWikiStore((s) => s.showOperationTree)
  useLayoutEffect(() => {
    if (operationId) showOperationTree(operationId)
  }, [operationId, showOperationTree])
  const expandedNodes = useWikiStore((s) =>
    s.expandedOperationId === operationId ? s.expandedNodes : NONE_EXPANDED,
  )

  // Roots — same query the sidebar used before (parentDocumentId: null).
  const { data: rootsData, isLoading: rootsLoading } = useWikiDocumentChildren(
    operationId,
    null,
  )

  const rootNodes = useMemo(
    () => sortByOrder(rootsData?.wikiDocumentChildren ?? []).map(rowToTreeNode),
    [rootsData?.wikiDocumentChildren],
  )

  // Stable, sorted id list so the useQueries array order is deterministic.
  const expandedIds = useMemo(
    () => [...expandedNodes].sort(),
    [expandedNodes],
  )

  const childQueries = useQueries({
    queries: expandedIds.map((id) =>
      wikiChildrenQueryOptions(operationId, id),
    ),
  })

  // parentId → sorted child nodes, for every expanded branch that has resolved.
  // Branches still loading are absent from the map → the walk emits a skeleton.
  //
  // Fingerprint on `dataUpdatedAt` (not just "has data?"): a move/reorder
  // refetches the affected parent slices with *changed content* but the branch
  // still has data, so a presence-only fingerprint would keep rendering the
  // stale child list (doc lingering under its old parent until reload).
  // dataUpdatedAt bumps on every successful (re)fetch — including the
  // post-mutation invalidation — so the map rebuilds whenever any slice changes.
  const childrenFingerprint = childQueries
    .map((q) => q.dataUpdatedAt)
    .join("|")
  const childrenByParent = useMemo(() => {
    const map = new Map<string, TreeNode[]>()
    expandedIds.forEach((id, i) => {
      const data = childQueries[i]?.data as
        | WikiDocumentChildrenQuery
        | undefined
      if (data) {
        map.set(id, sortByOrder(data.wikiDocumentChildren).map(rowToTreeNode))
      }
    })
    return map
    // childQueries is a fresh array each render; the fingerprint above captures
    // every slice change without rebuilding on unrelated parent re-renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [expandedIds, childrenFingerprint])

  // Each id restored from storage is checked once against the server, and
  // dropped if it has no children left (settleRestoredIds has the rule).
  // Otherwise the set only grows, and every load asks for the children of
  // every id it ever held. Ids expanded during the session are not checked:
  // a branch expanded right after a child is created or moved under it can
  // show a stale empty list until the change event lands.
  const unconfirmed = useWikiStore((s) => s.unconfirmedExpandedIds)
  const restoredAt = useWikiStore((s) => s.expandedRestoredAt)
  const settleExpanded = useWikiStore((s) => s.settleExpanded)
  const settled = useMemo(
    () =>
      settleRestoredIds(
        expandedIds.map((id, i) => {
          const q = childQueries[i]
          const data = q?.isSuccess
            ? (q.data as WikiDocumentChildrenQuery)
            : undefined
          return {
            id,
            childCount: data ? data.wikiDocumentChildren.length : null,
            fetchedAt: q?.dataUpdatedAt ?? 0,
          }
        }),
        unconfirmed,
        restoredAt,
      ),
    // Keyed on the same fingerprint as childrenByParent, for the same reason.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [expandedIds, childrenFingerprint, unconfirmed, restoredAt],
  )
  useEffect(() => {
    if (settled.confirmed.length > 0 || settled.dropped.length > 0) {
      settleExpanded(settled.confirmed, settled.dropped)
    }
  }, [settled, settleExpanded])

  const rows = useMemo(() => {
    const out: FlatRow[] = []
    const walk = (nodes: TreeNode[], depth: number) => {
      for (const node of nodes) {
        out.push({ kind: "node", key: node.id, node, depth })
        if (node.childCount > 0 && expandedNodes.has(node.id)) {
          const kids = childrenByParent.get(node.id)
          if (kids) {
            walk(kids, depth + 1)
          } else {
            out.push({
              kind: "skeleton",
              key: `skeleton:${node.id}`,
              depth: depth + 1,
            })
          }
        }
      }
    }
    walk(rootNodes, 0)
    return out
  }, [rootNodes, childrenByParent, expandedNodes])

  return { rows, rootsLoading }
}

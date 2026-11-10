import type { TreeNode } from "@/components/wiki/wiki-tree-sidebar"
import type { WikiDocumentTreeFieldsFragment } from "@/graphql/gql/graphql"

/**
 * Fixed pixel height of a single tree row. Node rows are `h-7` and skeleton
 * placeholders are `py-1` + `h-5` — both resolve to 28px. The virtualizer runs
 * in fixed-size mode and trusts this exactly (no per-row measurement), so any
 * change to the row's height must be mirrored here.
 */
export const WIKI_TREE_ROW_HEIGHT = 28

/**
 * Left padding (px) for a row at the given depth: a 4px base gutter plus 16px
 * per ancestor level. Shared by the row and its skeleton placeholder so the two
 * line up, and matched by the tree-connector-line geometry in wiki-tree-node.
 */
export function wikiRowIndent(depth: number): number {
  return depth * 16 + 4
}

/**
 * Walk a flat list of tree-row fragments and return every id under `rootId`
 * (inclusive of `rootId` itself when not null) whose row has children. Used
 * by "Expand all" / "Expand subtree" — the lazy tree has no knowledge of
 * unloaded branches, so the caller first primes the full tree (see
 * `useEnsureWikiTree`) and then hands the resulting flat list here.
 *
 * Pass `rootId = null` to walk the whole operation from the roots.
 */
export function collectExpandableIdsFromFlat(
  rows: readonly WikiDocumentTreeFieldsFragment[],
  rootId: string | null,
): string[] {
  // Build the parent → children index once so each subtree walk is O(n).
  const byParent = new Map<string | null, WikiDocumentTreeFieldsFragment[]>()
  for (const row of rows) {
    const pid = row.parentDocumentId ?? null
    const arr = byParent.get(pid) ?? []
    arr.push(row)
    byParent.set(pid, arr)
  }

  const out: string[] = []

  function visit(parentId: string | null) {
    const children = byParent.get(parentId) ?? []
    for (const child of children) {
      if (child.childCount > 0) {
        out.push(child.id)
        visit(child.id)
      }
    }
  }

  if (rootId === null) {
    visit(null)
    return out
  }

  // Scoped to a subtree — include the root itself when it has children, so
  // the caller's "Expand subtree on N" actually opens N.
  const root = rows.find((r) => r.id === rootId)
  if (root && root.childCount > 0) out.push(rootId)
  visit(rootId)
  return out
}

/**
 * Sort tree rows by their fractional sort order (lexicographic). The lazy
 * children query already returns rows in sort_order, but client-side
 * operations (DnD optimistic updates, palette filtering) reuse the comparator.
 */
export function sortByOrder<T extends { sortOrder: string }>(rows: readonly T[]): T[] {
  return [...rows].sort((a, b) =>
    a.sortOrder < b.sortOrder ? -1 : a.sortOrder > b.sortOrder ? 1 : 0,
  )
}

/**
 * Compute a sort string between two adjacent strings.
 * Uses simple midpoint character approach for fractional indexing.
 */
export function midSortOrder(before: string | null, after: string | null): string {
  const a = before ?? ""
  const b = after ?? ""
  const maxLen = Math.max(a.length, b.length) + 1
  let result = ""
  for (let i = 0; i < maxLen; i++) {
    const ca = i < a.length ? a.charCodeAt(i) : 64 // '@' — below 'A'
    const cb = i < b.length ? b.charCodeAt(i) : 123 // '{' — above 'z'
    const mid = Math.floor((ca + cb) / 2)
    if (mid > ca) {
      result += String.fromCharCode(mid)
      return result
    }
    result += String.fromCharCode(ca)
  }
  return result + "V" // fallback: append midpoint char
}

/**
 * Single-letter sortOrder for the i-th of `count` total siblings using the
 * A..z (65..122) bucketing shared by every sibling-rebalance path: DnD
 * before/after reorder, DnD "inside" drops, and the move dialog. Centralized
 * so the three callers can't drift apart on the bucket math.
 */
export function rebalancedSortOrderAt(index: number, count: number): string {
  const fraction = (index + 1) / (count + 1)
  return String.fromCharCode(65 + Math.floor(fraction * 57))
}

/**
 * Promote a flat tree-row fragment to the recursive TreeNode shape consumed
 * by the sidebar. `children` is initialized empty — lazy hooks fill it in
 * per-branch when a node expands. Shared by the sidebar's root render and
 * each WikiTreeNode's children render so the conversion stays one line.
 */
export function rowToTreeNode(row: WikiDocumentTreeFieldsFragment): TreeNode {
  return {
    id: row.id,
    title: row.title,
    emoji: row.emoji,
    icon: row.icon,
    color: row.color,
    sortOrder: row.sortOrder,
    parentId: row.parentDocumentId ?? null,
    childCount: row.childCount,
    isTemplate: row.isTemplate,
    kind: row.kind,
    pageType: row.pageType,
    tags: row.tags,
    status: row.status,
    children: [],
  }
}

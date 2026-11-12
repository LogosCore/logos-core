import { useEffect, useRef } from "react"
import { useWikiStore } from "@/stores/wiki"
import type { FlatRow } from "@/components/wiki/use-flattened-wiki-tree"

/**
 * The open page expanded in the tree: once when it opens (from the tree, a
 * link, search, back), and again when it gains its first child while open (an
 * agent adding one, say). Not on every render: a reader who collapses it keeps
 * it collapsed until they open another page (or click it in the tree again).
 * A page without children is never added to the expanded set, which is saved
 * and checked on the next load.
 */
export function useExpandSelectedRow(flatRows: FlatRow[]): void {
  const selectedDocumentId = useWikiStore((s) => s.selectedDocumentId)
  const expandNode = useWikiStore((s) => s.expandNode)
  // The page last seen open, and whether it had children then.
  const seen = useRef<SeenPage | null>(null)

  const row = selectedDocumentId
    ? flatRows.find((r) => r.kind === "node" && r.node.id === selectedDocumentId)
    : undefined
  // Unknown until its row is in the tree (its ancestors still loading).
  const hasChildren = row?.kind === "node" ? row.node.childCount > 0 : null

  useEffect(() => {
    if (!selectedDocumentId || hasChildren === null) return
    const now = { id: selectedDocumentId, hadChildren: hasChildren }
    const expand = expandsNow(seen.current, now)
    seen.current = now
    if (expand) expandNode(selectedDocumentId)
  }, [selectedDocumentId, hasChildren, expandNode])
}

/** The open page as last seen: its id, and whether it had children. */
export interface SeenPage {
  id: string
  hadChildren: boolean
}

/**
 * Whether the open page expands now: it has children, and it was not already
 * open with children (just opened, or its first child just came).
 */
export function expandsNow(last: SeenPage | null, now: SeenPage): boolean {
  if (!now.hadChildren) return false
  return !(last?.id === now.id && last.hadChildren)
}

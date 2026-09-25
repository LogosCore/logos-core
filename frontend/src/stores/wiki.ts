import { create } from "zustand"
import type { WikiDocumentSort } from "@/graphql/gql/graphql"

// Expanded tree nodes, one entry per operation: `wiki_expanded_nodes:<id>`.
const STORAGE_PREFIX_EXPANDED = "wiki_expanded_nodes:"
// Before that, one set under this key held every operation's ids, and nothing
// ever removed one. Each load asked for the children of all of them.
const LEGACY_STORAGE_KEY_EXPANDED = "wiki_expanded_nodes"
const STORAGE_KEY_WIDTH = "wiki_sidebar_width"
const STORAGE_KEY_RECENT_SORT = "wiki_recent_docs_sort"
const DEFAULT_WIDTH = 256
const DEFAULT_RECENT_SORT: WikiDocumentSort = "RECENTLY_CREATED"

function loadRecentSort(): WikiDocumentSort {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_RECENT_SORT)
    if (raw === "RECENTLY_CREATED" || raw === "RECENTLY_UPDATED") return raw
    return DEFAULT_RECENT_SORT
  } catch {
    return DEFAULT_RECENT_SORT
  }
}

function loadExpandedNodes(operationId: string): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX_EXPANDED + operationId)
    return raw ? new Set(JSON.parse(raw) as string[]) : new Set()
  } catch {
    return new Set()
  }
}

// Storage that is full or disabled only costs the tree its memory, so a
// failed write is ignored rather than failing the toggle.
function saveExpandedNodes(operationId: string | null, nodes: Set<string>) {
  if (!operationId) return
  try {
    const key = STORAGE_PREFIX_EXPANDED + operationId
    if (nodes.size === 0) localStorage.removeItem(key)
    else localStorage.setItem(key, JSON.stringify([...nodes]))
  } catch {
    // ignored, see above
  }
}

try {
  localStorage.removeItem(LEGACY_STORAGE_KEY_EXPANDED)
} catch {
  // Storage is unavailable; there is nothing to clean up either.
}

/** One expanded id's children query, as useFlattenedWikiTree sees it. */
export interface ExpandedBranch {
  id: string
  /** How many children the query returned; null until it has succeeded. */
  childCount: number | null
  /** When that answer was fetched (React Query's dataUpdatedAt). */
  fetchedAt: number
}

/**
 * The restored ids a round of children queries settles, for settleExpanded:
 * only ids still unconfirmed, and only on an answer fetched after they were
 * restored, since a cached one may predate a change. An id with no children
 * is a document since deleted or a folder since emptied, and is dropped.
 */
export function settleRestoredIds(
  branches: readonly ExpandedBranch[],
  unconfirmed: ReadonlySet<string>,
  restoredAt: number,
): { confirmed: string[]; dropped: string[] } {
  const confirmed: string[] = []
  const dropped: string[] = []
  for (const { id, childCount, fetchedAt } of branches) {
    if (!unconfirmed.has(id) || childCount === null || fetchedAt < restoredAt) {
      continue
    }
    if (childCount === 0) dropped.push(id)
    else confirmed.push(id)
  }
  return { confirmed, dropped }
}

function loadSidebarWidth(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_WIDTH)
    return raw ? Number(raw) : DEFAULT_WIDTH
  } catch {
    return DEFAULT_WIDTH
  }
}

export interface BackupConfirmTarget {
  backupId: string
  documentId: string
  action: "restore" | "delete"
  // Meta carried from the list row so the confirm dialog can show a
  // specific, contextful message without re-fetching.
  createdAt: string
  trigger: "AUTO" | "MANUAL"
  description: string
}

interface WikiStoreState {
  // Tree expand/collapse. The set belongs to the operation whose tree is on
  // screen: showOperationTree swaps it for that operation's own, which is
  // what the actions below then read and persist.
  expandedOperationId: string | null
  expandedNodes: Set<string>
  showOperationTree: (operationId: string) => void
  // Ids restored from storage that the server has not answered for yet, and
  // when they were restored. useFlattenedWikiTree checks each one once and
  // reports back through settleExpanded: an id whose children come back
  // empty (a document since deleted, a folder since emptied) is dropped.
  unconfirmedExpandedIds: ReadonlySet<string>
  expandedRestoredAt: number
  settleExpanded: (confirmed: readonly string[], dropped: readonly string[]) => void
  toggleNode: (id: string) => void
  expandNode: (id: string) => void
  expandMany: (ids: readonly string[]) => void
  collapseMany: (ids: readonly string[]) => void

  // Currently-selected document (mirrors the :documentId route param). Kept in
  // the store — rather than read via useParams in each row — so a tree row can
  // subscribe to a *boolean* (selectedDocumentId === node.id). On navigation
  // only the two rows whose selection flips re-render; reading useParams in
  // every row re-rendered the whole (potentially 300+ row) tree on each open,
  // stalling the editor mount behind it. Synced once at the page level.
  selectedDocumentId: string | null
  setSelectedDocumentId: (id: string | null) => void

  // Create dialog
  createDialogOpen: boolean
  createParentId: string | null
  openCreateDialog: (parentId?: string | null) => void
  closeCreateDialog: () => void

  // Import dialog (Logos bundle or markdown zip)
  importDialogOpen: boolean
  openImportDialog: () => void
  closeImportDialog: () => void

  // Export dialog. `target` is null for tree-wide exports, populated for
  // subtree exports triggered from the 3-dots menu on a tree row.
  exportDialogOpen: boolean
  exportTarget: { id: string; title: string; childCount: number } | null
  openExportDialog: (
    target?: { id: string; title: string; childCount: number },
  ) => void
  closeExportDialog: () => void

  // Move dialog
  moveDialogOpen: boolean
  moveTarget: { id: string; title: string } | null
  openMoveDialog: (target: { id: string; title: string }) => void
  closeMoveDialog: () => void

  // Delete dialog (soft delete)
  deleteDialogOpen: boolean
  deleteTarget: { id: string; title: string } | null
  openDeleteDialog: (target: { id: string; title: string }) => void
  closeDeleteDialog: () => void

  // Duplicate dialog — only opens for documents with children, so the user
  // can choose whether the clone is shallow or deep. Leaf documents bypass
  // the dialog entirely.
  duplicateDialogOpen: boolean
  duplicateTarget: { id: string; title: string; childCount: number } | null
  openDuplicateDialog: (target: { id: string; title: string; childCount: number }) => void
  closeDuplicateDialog: () => void

  // Permanent delete dialog
  permanentDeleteDialogOpen: boolean
  permanentDeleteTarget: { id: string; title: string } | null
  openPermanentDeleteDialog: (target: { id: string; title: string }) => void
  closePermanentDeleteDialog: () => void

  // Trash panel
  trashPanelOpen: boolean
  openTrashPanel: () => void
  closeTrashPanel: () => void

  // Backup panel
  backupPanelOpen: boolean
  backupDocumentId: string | null
  openBackupPanel: (documentId: string) => void
  closeBackupPanel: () => void

  // Backup preview dialog (row click → side-by-side view)
  backupPreviewId: string | null
  openBackupPreview: (backupId: string) => void
  closeBackupPreview: () => void

  // Backup confirm dialog (restore / delete, with embedded context)
  backupConfirmTarget: BackupConfirmTarget | null
  openBackupConfirm: (target: BackupConfirmTarget) => void
  closeBackupConfirm: () => void

  // Recent documents modal — sort preference persists across sessions so
  // the user's last toggle choice survives a reload.
  recentDocsOpen: boolean
  recentDocsSort: WikiDocumentSort
  openRecentDocs: () => void
  closeRecentDocs: () => void
  setRecentDocsSort: (sort: WikiDocumentSort) => void

  // Sidebar width
  sidebarWidth: number
  setSidebarWidth: (width: number) => void

  // Editor zoom (focus mode — overlays the wiki tree and app sidebar)
  editorZoomed: boolean
  toggleEditorZoom: () => void
  setEditorZoom: (zoomed: boolean) => void

  // Editor table-of-contents overlay (floats over the editor's upper-right
  // corner). Transient view state — not persisted; reverts to hidden on each
  // navigation/load so the panel doesn't surprise users who never opened it.
  editorTocVisible: boolean
  toggleEditorToc: () => void
  setEditorTocVisible: (visible: boolean) => void

  // One-shot signal from the create flow to the editor: when the editor for
  // this document mounts, focus it at the start so the user can start typing
  // immediately. Consumed (cleared) by the editor on first apply so revisits
  // don't keep stealing focus.
  pendingFocusDocId: string | null
  setPendingFocusDocId: (id: string | null) => void
}

export const useWikiStore = create<WikiStoreState>((set, get) => ({
  // Tree expand/collapse — persisted to localStorage per operation
  expandedOperationId: null,
  expandedNodes: new Set(),
  unconfirmedExpandedIds: new Set(),
  expandedRestoredAt: 0,
  showOperationTree: (operationId) => {
    if (get().expandedOperationId === operationId) return
    const restored = loadExpandedNodes(operationId)
    set({
      expandedOperationId: operationId,
      expandedNodes: restored,
      unconfirmedExpandedIds: new Set(restored),
      expandedRestoredAt: Date.now(),
    })
  },
  settleExpanded: (confirmed, dropped) => {
    const unconfirmed = new Set(get().unconfirmedExpandedIds)
    for (const id of [...confirmed, ...dropped]) unconfirmed.delete(id)
    if (dropped.length === 0) {
      set({ unconfirmedExpandedIds: unconfirmed })
      return
    }
    const next = new Set(get().expandedNodes)
    for (const id of dropped) next.delete(id)
    saveExpandedNodes(get().expandedOperationId, next)
    set({ unconfirmedExpandedIds: unconfirmed, expandedNodes: next })
  },
  toggleNode: (id) => {
    const next = new Set(get().expandedNodes)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    saveExpandedNodes(get().expandedOperationId, next)
    set({ expandedNodes: next })
  },
  expandNode: (id) => {
    const next = new Set(get().expandedNodes)
    next.add(id)
    saveExpandedNodes(get().expandedOperationId, next)
    set({ expandedNodes: next })
  },
  expandMany: (ids) => {
    const next = new Set(get().expandedNodes)
    for (const id of ids) next.add(id)
    saveExpandedNodes(get().expandedOperationId, next)
    set({ expandedNodes: next })
  },
  collapseMany: (ids) => {
    const next = new Set(get().expandedNodes)
    for (const id of ids) next.delete(id)
    saveExpandedNodes(get().expandedOperationId, next)
    set({ expandedNodes: next })
  },

  // Selected document — transient, mirrors the route param. Guard the set so
  // navigating to the same doc doesn't churn the store (and its boolean
  // subscribers) needlessly.
  selectedDocumentId: null,
  setSelectedDocumentId: (id) => {
    if (get().selectedDocumentId !== id) set({ selectedDocumentId: id })
  },

  // Create dialog
  createDialogOpen: false,
  createParentId: null,
  openCreateDialog: (parentId) =>
    set({ createDialogOpen: true, createParentId: parentId ?? null }),
  closeCreateDialog: () =>
    set({ createDialogOpen: false, createParentId: null }),

  // Import dialog
  importDialogOpen: false,
  openImportDialog: () => set({ importDialogOpen: true }),
  closeImportDialog: () => set({ importDialogOpen: false }),

  // Export dialog
  exportDialogOpen: false,
  exportTarget: null,
  openExportDialog: (target) =>
    set({ exportDialogOpen: true, exportTarget: target ?? null }),
  closeExportDialog: () =>
    set({ exportDialogOpen: false, exportTarget: null }),

  // Move dialog
  moveDialogOpen: false,
  moveTarget: null,
  openMoveDialog: (target) =>
    set({ moveDialogOpen: true, moveTarget: target }),
  closeMoveDialog: () =>
    set({ moveDialogOpen: false, moveTarget: null }),

  // Delete dialog
  deleteDialogOpen: false,
  deleteTarget: null,
  openDeleteDialog: (target) =>
    set({ deleteDialogOpen: true, deleteTarget: target }),
  closeDeleteDialog: () =>
    set({ deleteDialogOpen: false, deleteTarget: null }),

  // Duplicate dialog
  duplicateDialogOpen: false,
  duplicateTarget: null,
  openDuplicateDialog: (target) =>
    set({ duplicateDialogOpen: true, duplicateTarget: target }),
  closeDuplicateDialog: () =>
    set({ duplicateDialogOpen: false, duplicateTarget: null }),

  // Permanent delete dialog
  permanentDeleteDialogOpen: false,
  permanentDeleteTarget: null,
  openPermanentDeleteDialog: (target) =>
    set({ permanentDeleteDialogOpen: true, permanentDeleteTarget: target }),
  closePermanentDeleteDialog: () =>
    set({ permanentDeleteDialogOpen: false, permanentDeleteTarget: null }),

  // Trash panel
  trashPanelOpen: false,
  openTrashPanel: () => set({ trashPanelOpen: true }),
  closeTrashPanel: () => set({ trashPanelOpen: false }),

  // Backup panel
  backupPanelOpen: false,
  backupDocumentId: null,
  openBackupPanel: (documentId) =>
    set({ backupPanelOpen: true, backupDocumentId: documentId }),
  closeBackupPanel: () =>
    set({ backupPanelOpen: false, backupDocumentId: null }),

  // Backup preview dialog
  backupPreviewId: null,
  openBackupPreview: (backupId) => set({ backupPreviewId: backupId }),
  closeBackupPreview: () => set({ backupPreviewId: null }),

  // Backup confirm dialog
  backupConfirmTarget: null,
  openBackupConfirm: (target) => set({ backupConfirmTarget: target }),
  closeBackupConfirm: () => set({ backupConfirmTarget: null }),

  // Recent documents modal
  recentDocsOpen: false,
  recentDocsSort: loadRecentSort(),
  openRecentDocs: () => set({ recentDocsOpen: true }),
  closeRecentDocs: () => set({ recentDocsOpen: false }),
  setRecentDocsSort: (sort) => {
    localStorage.setItem(STORAGE_KEY_RECENT_SORT, sort)
    set({ recentDocsSort: sort })
  },

  // Sidebar width — persisted to localStorage
  sidebarWidth: loadSidebarWidth(),
  setSidebarWidth: (width) => {
    localStorage.setItem(STORAGE_KEY_WIDTH, String(width))
    set({ sidebarWidth: width })
  },

  // Editor zoom — transient view state, not persisted.
  editorZoomed: false,
  toggleEditorZoom: () => set((state) => ({ editorZoomed: !state.editorZoomed })),
  setEditorZoom: (zoomed) => set({ editorZoomed: zoomed }),

  // Editor TOC — transient view state, not persisted.
  editorTocVisible: false,
  toggleEditorToc: () => set((state) => ({ editorTocVisible: !state.editorTocVisible })),
  setEditorTocVisible: (visible) => set({ editorTocVisible: visible }),

  // Editor caret bootstrap — set by the create dialog right before it
  // navigates to the new doc.
  pendingFocusDocId: null,
  setPendingFocusDocId: (id) => set({ pendingFocusDocId: id }),
}))

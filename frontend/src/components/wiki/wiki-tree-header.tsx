import { useState, useTransition } from "react"
import { toast } from "sonner"
import {
  ArrowDownAZIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  ClockIcon,
  DownloadIcon,
  EllipsisIcon,
  HistoryIcon,
  Loader2Icon,
  PlusIcon,
  SearchIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Popover, PopoverContent } from "@/components/ui/popover"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useWikiStore } from "@/stores/wiki"
import { openWikiSearch } from "@/components/wiki/wiki-palette-store"
import {
  useReorderWikiDocumentSiblings,
  useWikiDocumentChildren,
  useWikiDocumentTrashCount,
} from "@/graphql/hooks/wiki"
import { WikiHistoryPanel } from "@/components/wiki/wiki-history-panel"
import { WikiTreeModeToggle } from "@/components/wiki/wiki-tree-mode-toggle"
import { useWikiSubtreeExpansion } from "@/components/wiki/use-wiki-subtree-expansion"
import { sortByOrder } from "@/components/wiki/wiki-tree-helpers"

interface WikiTreeHeaderProps {
  operationId: string
  isEditor: boolean
  /** True when there's a real scoped operation — drives the mode toggle. */
  hasRealScope: boolean
  /** Display name of the scoped operation, for the toggle's tooltip. */
  operationName?: string | null
}

/**
 * The tree sidebar's header row.
 *
 * Ten icon buttons used to sit here, which read as a wall of glyphs in a
 * 280px-wide column and left no room for the thing the row is actually
 * titled by. Four remain: expand/collapse, search, new document, and an
 * overflow menu holding everything else. Nothing was dropped — the menu
 * lists every action the toolbar used to, by name rather than by glyph,
 * which is the more legible form for the ones reached a few times a week.
 *
 * Self-contained on purpose: the queries it reads (roots, trash count) are
 * the same cache entries the tree itself subscribes to, so asking for them
 * here costs no extra round-trip and keeps the sidebar to its one job.
 */
export function WikiTreeHeader({
  operationId,
  isEditor,
  hasRealScope,
  operationName,
}: WikiTreeHeaderProps) {
  const openCreateDialog = useWikiStore((s) => s.openCreateDialog)
  const openImportDialog = useWikiStore((s) => s.openImportDialog)
  const openExportDialog = useWikiStore((s) => s.openExportDialog)
  const openTrashPanel = useWikiStore((s) => s.openTrashPanel)
  const openRecentDocs = useWikiStore((s) => s.openRecentDocs)
  const collapseMany = useWikiStore((s) => s.collapseMany)
  // Drives which way the single expand/collapse button points. A boolean
  // selector rather than the Set itself, so the header doesn't re-render on
  // every individual branch toggle.
  const anyExpanded = useWikiStore((s) => s.expandedNodes.size > 0)

  // Scalar query, not the full list.
  const { data: trashCountData } = useWikiDocumentTrashCount(operationId)
  const trashCount = trashCountData?.wikiDocumentTrashCount ?? 0

  // Shared cache entry with the tree — used only to decide whether sorting
  // the roots is a meaningful offer.
  const { data: rootsData } = useWikiDocumentChildren(operationId, null)
  const roots = rootsData?.wikiDocumentChildren ?? []

  // "Expand all" primes the full operation tree (one cached GraphQL fetch)
  // and expands every non-leaf id — including branches the user has never
  // opened. The hook also covers the React commit phase via useTransition.
  const { loading: expandAllLoading, run: runSubtreeAction } =
    useWikiSubtreeExpansion(operationId)

  // Collapse-all has no fetch phase — `expandedNodes` already lists every id
  // we need to drop. Just a transition around the unmount commit.
  const [isCollapsing, startCollapseTransition] = useTransition()
  const busy = expandAllLoading || isCollapsing

  const reorderSiblings = useReorderWikiDocumentSiblings()

  // History is a panel anchored to the overflow button that opened it: the
  // menu item that reveals it has closed the menu by the time it renders,
  // so the popover borrows the trigger's position rather than its own.
  //
  // The anchor is a wrapper around the button rather than the button itself.
  // A ref on the button would have to survive two nested `render` props
  // (Tooltip → menu trigger → Button), and it does not: the popover reads a
  // null anchor and positions itself in the corner of the viewport. The
  // wrapper is a callback-ref element in state, so the popover also re-reads
  // it once it mounts instead of caching the first null.
  const [moreAnchor, setMoreAnchor] = useState<HTMLSpanElement | null>(null)
  const [historyOpen, setHistoryOpen] = useState(false)

  function handleCollapseAll() {
    if (isCollapsing) return
    const ids = [...useWikiStore.getState().expandedNodes]
    if (ids.length === 0) return
    startCollapseTransition(() => collapseMany(ids))
  }

  function handleToggleExpandAll() {
    if (busy) return
    if (anyExpanded) handleCollapseAll()
    else void runSubtreeAction("expand", null)
  }

  // Sort the root documents alphabetically. Mirrors the per-node "Sort"
  // action in wiki-tree-row-menu-items.tsx but targets parentDocumentId: null.
  function handleSortRoots() {
    if (roots.length < 2) return
    const sorted = [...sortByOrder(roots)].sort((a, b) =>
      a.title.localeCompare(b.title, undefined, { sensitivity: "base" }),
    )
    reorderSiblings.mutate(
      {
        input: {
          operationId,
          parentDocumentId: null,
          orderedIds: sorted.map((d) => d.id),
        },
      },
      {
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : "Failed to sort"),
      },
    )
  }

  return (
    <div className="flex h-10 items-center gap-0.5 border-b px-2">
      {/* The mode toggle takes the title slot: which tree you are looking at
          is the one thing this row has to say, and the segment selection
          says it without spending a line on a heading. */}
      <WikiTreeModeToggle
        hasRealScope={hasRealScope}
        operationName={operationName}
      />
      <span className="flex-1 min-w-1" />

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={handleToggleExpandAll}
              disabled={busy}
              aria-label={anyExpanded ? "Collapse all" : "Expand all"}
            />
          }
        >
          {busy ? (
            <Loader2Icon className="size-3.5 animate-spin" />
          ) : anyExpanded ? (
            <ChevronsDownUpIcon className="size-3.5" />
          ) : (
            <ChevronsUpDownIcon className="size-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>
          {expandAllLoading
            ? "Expanding…"
            : isCollapsing
              ? "Collapsing…"
              : anyExpanded
                ? "Collapse all"
                : "Expand all"}
        </TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="Search documents"
              onClick={() =>
                openWikiSearch({
                  operationId,
                  parentDocumentId: null,
                  parentTitle: "All Documents",
                })
              }
            />
          }
        >
          <SearchIcon className="size-3.5" />
        </TooltipTrigger>
        <TooltipContent>Search documents</TooltipContent>
      </Tooltip>

      {isEditor && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="New document"
                onClick={() => openCreateDialog()}
              />
            }
          >
            <PlusIcon className="size-3.5" />
          </TooltipTrigger>
          <TooltipContent>New document</TooltipContent>
        </Tooltip>
      )}

      {/* The span wraps exactly the trigger button, so its box is the box the
          history panel is positioned against. */}
      <span ref={setMoreAnchor} className="inline-flex">
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger
              render={
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="More actions"
                    />
                  }
                >
                  <EllipsisIcon className="size-3.5" />
                </DropdownMenuTrigger>
              }
            />
            <TooltipContent>More</TooltipContent>
          </Tooltip>
          <DropdownMenuContent
            // Leading-edge alignment: the menu hangs off the left edge of the
            // button that opened it and runs rightwards over the editor, which
            // is also how the History panel below opens. Trailing-edge would
            // keep both inside the sidebar, but it pins the far corner to the
            // button and leaves the near one adrift.
            align="start"
            // w-auto: the anchor is a 24px icon button, and the default
            // `w-(--anchor-width)` would size the menu to it.
            className="w-auto min-w-52"
            // When the History item is what closed this menu, don't pull focus
            // back to the trigger: the popover that just opened has taken it,
            // and a focus-out is one of the reasons a popover closes — the
            // panel would vanish the moment it appeared.
            finalFocus={historyOpen ? false : undefined}
          >
            {/* Both directions listed even though the toolbar button toggles:
                from a half-expanded tree the button only offers one of them,
                and the other is the one you wanted. */}
            <DropdownMenuItem
              onClick={() => void runSubtreeAction("expand", null)}
              disabled={busy}
            >
              <ChevronsUpDownIcon className="mr-2 size-4" />
              Expand all
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={handleCollapseAll}
              disabled={busy || !anyExpanded}
            >
              <ChevronsDownUpIcon className="mr-2 size-4" />
              Collapse all
            </DropdownMenuItem>
            {isEditor && roots.length >= 2 && (
              <DropdownMenuItem
                onClick={handleSortRoots}
                disabled={reorderSiblings.isPending}
              >
                <ArrowDownAZIcon className="mr-2 size-4" />
                Sort root documents A–Z
              </DropdownMenuItem>
            )}

            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => setHistoryOpen(true)}>
              <HistoryIcon className="mr-2 size-4" />
              History
            </DropdownMenuItem>
            <DropdownMenuItem onClick={openRecentDocs}>
              <ClockIcon className="mr-2 size-4" />
              Latest documents
            </DropdownMenuItem>

            <DropdownMenuSeparator />
            {isEditor && (
              <DropdownMenuItem onClick={openImportDialog}>
                <DownloadIcon className="mr-2 size-4" />
                Import wiki…
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={() => openExportDialog()}>
              <UploadIcon className="mr-2 size-4" />
              Export wiki…
            </DropdownMenuItem>

            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={openTrashPanel}>
              <Trash2Icon className="mr-2 size-4" />
              Trash
              {trashCount > 0 && (
                <Badge
                  variant="secondary"
                  className="ml-auto h-4 justify-center px-1.5 text-[10px]"
                >
                  {trashCount > 99 ? "99+" : trashCount}
                </Badge>
              )}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </span>

      {/* Anchored to the overflow button, not to a trigger of its own — the
          only way in is the menu item above. */}
      <Popover open={historyOpen} onOpenChange={setHistoryOpen}>
        <PopoverContent
          anchor={moreAnchor}
          // Same leading-edge alignment as the menu this opens from, so both
          // grow rightwards from the same corner of the same button. It matters
          // more here: the panel is 26rem, wider than the sidebar itself, so
          // trailing-edge alignment swept it across the whole tree with only
          // its far corner near the button. If the window is too narrow to fit
          // it to the right, the positioner shifts it back into view on its own.
          align="start"
          // No trigger of its own means no default place for focus to land on
          // close, so name the button inside the anchor.
          finalFocus={() => moreAnchor?.querySelector("button") ?? false}
          className="w-[26rem] gap-0 p-0"
        >
          <WikiHistoryPanel
            operationId={operationId}
            open={historyOpen}
            onClose={() => setHistoryOpen(false)}
          />
        </PopoverContent>
      </Popover>
    </div>
  )
}

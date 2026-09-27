import { cn } from "@/lib/utils"
import { WikiChildDocumentList } from "@/components/wiki/wiki-child-document-list"
import { WikiBacklinkList } from "@/components/wiki/wiki-backlink-list"
import { WikiTaskBacklinkList } from "@/components/wiki/wiki-task-backlink-list"
import type { WikiFooterSurface } from "@/stores/wiki-footer-sections"

interface WikiDocumentFooterListsProps {
  documentId: string
  operationId: string
  isEditor: boolean
  /** Which pane is hosting the footer. Decides whether each section starts
   * open, and keeps a drawing's folds from following the operator onto prose
   * pages — see the store's DEFAULT_OPEN. */
  surface: WikiFooterSurface
  /** Outer spacing, for a pane that composes this differently. The default
   * top margin separates the footer from the end of a page's prose; a drawing
   * pane puts it under a hard-edged canvas, where that gap is just dead
   * space. */
  className?: string
}

/**
 * Combined "Sub-pages + Backlinks + Task backlinks" footer block. The three
 * lists share a border-divider header and a container-query grid so they sit
 * side-by-side when the editor pane is wide, fall to two columns on medium
 * panes, and stack on narrow ones. Each is independently collapsible, and
 * remembers its fold per surface for the viewer.
 *
 * Container queries (`@container/footer` + `@3xl/footer:grid-cols-2`
 * + `@5xl/footer:grid-cols-3`) instead of viewport media queries because the
 * editor pane width is independent of the viewport — sidebar collapse,
 * resize, and zoom-mode all shift it without touching window size. The
 * breakpoints aim at "two columns once each list has comfortable row width,
 * three once the pane could host all three without cramping any of them."
 *
 * The grid is the layout in every state, folded or not: a section holds its
 * column whether or not its rows are showing, so folding one does not move its
 * siblings and the headers stay on the same vertical rules they had open. The
 * height of a folded footer is bought back in the section itself instead — a
 * folded CollapsibleSection is exactly its header tall — and in the tightened
 * rule above, not by repacking the row.
 */
export function WikiDocumentFooterLists({
  documentId,
  operationId,
  isEditor,
  surface,
  className,
}: WikiDocumentFooterListsProps) {
  return (
    <div className={cn("@container/footer mt-6 border-t pt-3", className)}>
      <div className="grid grid-cols-1 gap-x-8 gap-y-5 @3xl/footer:grid-cols-2 @5xl/footer:grid-cols-3">
        <WikiChildDocumentList
          documentId={documentId}
          operationId={operationId}
          isEditor={isEditor}
          surface={surface}
        />
        <WikiBacklinkList documentId={documentId} surface={surface} />
        <WikiTaskBacklinkList
          documentId={documentId}
          operationId={operationId}
          surface={surface}
        />
      </div>
    </div>
  )
}

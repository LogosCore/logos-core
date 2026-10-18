import { FileTextIcon } from "lucide-react"
import { Skeleton } from "@/components/ui/skeleton"
import { useWikiStore } from "@/stores/wiki"
import { cn } from "@/lib/utils"
import { WikiEditorPane } from "@/components/wiki/wiki-editor-pane"
import { WikiDrawingPane } from "@/components/wiki/drawing/wiki-drawing-pane"
import { useWikiDocument } from "@/graphql/hooks/wiki"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"

interface WikiContentAreaProps {
  documentId: string | null
  operationId: string
  isEditor: boolean
  /** An operation is scoped (the tree may still show Public by toggle). */
  hasRealScope: boolean
}

// Content area for the wiki page. Search used to live here inline; it's now
// a floating command palette (see WikiCommandPalette) that doesn't cover the
// editor while typing.
export function WikiContentArea({
  documentId,
  operationId,
  isEditor,
  hasRealScope,
}: WikiContentAreaProps) {
  // The kind decides which body to mount, so it is read here rather than
  // inside either pane. Both panes query the document again for their own
  // chrome; that second read is served from the query cache.
  const { data, isPending, isError } = useWikiDocument(documentId ?? "")

  if (documentId) {
    // Until the kind is known, neither pane: each opens the page's room as it
    // mounts, so mounting the prose pane on a guess cost every uncached
    // drawing a second ticket, socket and full sync of its scene when the
    // guess was replaced. A page that fails to load still gets the prose
    // pane, which is what says "Document not found".
    if (isPending && !isError) return <PaneSkeleton />

    if (data?.wikiDocument?.kind === "DRAWING") {
      return (
        <WikiDrawingPane
          documentId={documentId}
          operationId={operationId}
          isEditor={isEditor}
        />
      )
    }

    return (
      <WikiEditorPane
        documentId={documentId}
        operationId={operationId}
        isEditor={isEditor}
      />
    )
  }

  // / lands here, so with nothing scoped the empty pane is where a new
  // operator is told what to do. Only the pane: the sidebar keeps the Public
  // tree, which needs no operation, and Public deep links above still open.
  // Choosing an operation swaps the tree in place — no navigation.
  if (!hasRealScope) return <GettingStartedPanel publicWikiInView />

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground">
      <FileTextIcon className="size-10 opacity-40" />
      <p className="text-sm">Select or create a document</p>
    </div>
  )
}

/** A page whose kind is not known yet: the pane's frame and a title, and
 * nothing that would read as either prose or a canvas. */
function PaneSkeleton() {
  // Covers the app in focus mode, as both panes do, so opening a page while
  // zoomed does not flash the sidebar and tree for a moment.
  const editorZoomed = useWikiStore((s) => s.editorZoomed)
  return (
    <div
      className={cn(
        "flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border bg-card",
        editorZoomed && "fixed inset-0 z-40 rounded-none border-0",
      )}
    >
      <div className="flex items-center gap-3 p-4">
        <Skeleton className="size-8 rounded" />
        <Skeleton className="h-7 w-64" />
      </div>
    </div>
  )
}

import { FileTextIcon } from "lucide-react"
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
  const { data } = useWikiDocument(documentId ?? "")

  if (documentId) {
    // Until the kind is known, mount the prose pane: it is what every page was
    // before drawings existed, and it renders its own loading state. Guessing
    // the other way would flash a canvas onto every page open.
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

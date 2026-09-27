import { Component, useEffect, type ErrorInfo, type ReactNode } from "react"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { useWikiDocument } from "@/graphql/hooks/wiki"
import { WikiEditorHeader } from "@/components/wiki/wiki-editor-header"
import { WikiForeignOperationBanner } from "@/components/wiki/wiki-foreign-operation-banner"
import { WikiDocumentMeta } from "@/components/wiki/wiki-document-meta"
import { WikiEditor } from "@/components/wiki/wiki-editor"
import { WikiDocumentFooterLists } from "@/components/wiki/wiki-document-footer-lists"
import { useWikiStore } from "@/stores/wiki"
import { cn } from "@/lib/utils"

interface WikiEditorPaneProps {
  documentId: string
  operationId: string
  isEditor: boolean
}

export function WikiEditorPane({
  documentId,
  operationId,
  isEditor,
}: WikiEditorPaneProps) {
  const { data, isLoading, error } = useWikiDocument(documentId)
  const document = data?.wikiDocument
  const editorZoomed = useWikiStore((s) => s.editorZoomed)
  const setEditorZoom = useWikiStore((s) => s.setEditorZoom)

  // Zoom is dropped when the wiki page itself unmounts, in pages/wiki.tsx.
  // It cannot be done here: this pane also unmounts when a document's kind
  // turns out to be DRAWING and WikiContentArea swaps in the other pane,
  // which would drop focus mode on the way to a drawing.

  // Esc to exit zoom. Tiptap-internal popovers (slash menu, bubble menu)
  // consume Escape first via stopPropagation, so this only fires when nothing
  // inside the editor is claiming the key.
  useEffect(() => {
    if (!editorZoomed) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setEditorZoom(false)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [editorZoomed, setEditorZoom])

  return (
    // One wrapper for all three states. The zoom class has to be on whatever
    // is on screen at every instant: focus mode is this element covering the
    // app with `fixed inset-0`, so a loading or error state rendered outside
    // it uncovers the sidebar and tree for as long as it shows — which is
    // exactly one document fetch on every navigation, seen as a flash of the
    // layout you just zoomed away from.
    <div
      className={cn(
        "flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border bg-card",
        // z-40 so backup sheet, dialogs, and command palette (z-50) still
        // stack above when opened from inside the zoomed editor.
        editorZoomed && "fixed inset-0 z-40 rounded-none border-0",
      )}
    >
      {isLoading ? (
        <div className="flex flex-1 flex-col gap-4 p-4">
          <div className="flex items-center gap-3">
            <Skeleton className="size-8 rounded" />
            <Skeleton className="h-7 w-64" />
          </div>
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-4/6" />
        </div>
      ) : error || !document ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
          <p className="text-sm">Document not found</p>
        </div>
      ) : (
        <>
          <WikiEditorHeader
            document={document}
            operationId={operationId}
            isEditor={isEditor}
          />
          <WikiForeignOperationBanner document={document} />
          <WikiDocumentMeta document={document} />
          <EditorErrorBoundary documentId={documentId}>
            <WikiEditor
              documentId={documentId}
              operationId={operationId}
              isEditor={isEditor}
              footer={
                <WikiDocumentFooterLists
                  documentId={documentId}
                  operationId={operationId}
                  isEditor={isEditor}
                  surface="document"
                />
              }
            />
          </EditorErrorBoundary>
        </>
      )}
    </div>
  )
}

// Error boundary — catches editor crashes and shows a recovery UI
// instead of white-screening the entire wiki page.

interface ErrorBoundaryProps {
  documentId: string
  children: ReactNode
}

interface ErrorBoundaryState {
  error: Error | null
  documentId: string
}

class EditorErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null, documentId: this.props.documentId }

  static getDerivedStateFromProps(
    props: ErrorBoundaryProps,
    state: ErrorBoundaryState,
  ): Partial<ErrorBoundaryState> | null {
    if (props.documentId !== state.documentId) {
      return { error: null, documentId: props.documentId }
    }
    return null
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Wiki editor crashed:", error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return <EditorErrorFallback onRetry={() => this.setState({ error: null })} />
    }
    return this.props.children
  }
}

function EditorErrorFallback({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 text-muted-foreground">
      <p className="text-sm">Something went wrong loading the editor.</p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        Try again
      </Button>
    </div>
  )
}

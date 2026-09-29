import { useMemo, useRef, useState } from "react"
import { Link } from "react-router"
import {
  ChevronRightIcon,
  ClockIcon,
  EllipsisIcon,
  ListTreeIcon,
  Maximize2Icon,
  Minimize2Icon,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import {
  useUpdateWikiDocument,
  useWikiDocumentChildren,
} from "@/graphql/hooks/wiki"
import { useWikiStore } from "@/stores/wiki"
import {
  DocumentIconPicker,
  type DocumentIconValue,
} from "@/components/wiki/document-icon-picker"
import { DocumentIcon } from "@/components/wiki/document-icon"
import { WikiExportMenu } from "@/components/wiki/wiki-export-menu"
import { WikiEditorToc } from "@/components/wiki/wiki-editor-toc"
import { WikiPresenceMenu } from "@/components/wiki/wiki-presence-menu"
import { useActiveWikiEditor } from "@/components/wiki/wiki-active-editor"
import { WikiStatusBadge } from "@/components/wiki/wiki-document-meta"
import type { WikiDocumentFieldsFragment } from "@/graphql/gql/graphql"

interface WikiEditorHeaderProps {
  document: WikiDocumentFieldsFragment
  operationId: string
  isEditor: boolean
}

interface AncestorNode {
  id: string
  title: string
  emoji: string
  icon: string
  color: string
  /** A drawing renders a fixed glyph rather than the stored icon. */
  kind?: string | null
}

export function WikiEditorHeader({
  document: doc,
  operationId,
  isEditor,
}: WikiEditorHeaderProps) {
  const updateDocument = useUpdateWikiDocument()
  const openBackupPanel = useWikiStore((s) => s.openBackupPanel)
  const editorZoomed = useWikiStore((s) => s.editorZoomed)
  const toggleEditorZoom = useWikiStore((s) => s.toggleEditorZoom)
  const zoomLabel = editorZoomed ? "Exit fullscreen" : "Zoom in"

  // The live tiptap instance, published by WikiEditor — this header is its
  // sibling, not its parent, so there is no prop path. Null on a drawing
  // page and until the editor is ready, which is exactly when there is no
  // outline to offer.
  const activeEditor = useActiveWikiEditor()

  // Inline title editing.
  const [title, setTitle] = useState(doc.title)
  const [isEditingTitle, setIsEditingTitle] = useState(false)
  const titleRef = useRef<HTMLInputElement>(null)

  // Breadcrumb path: comes baked into the document payload via the server's
  // ancestors resolver. No need to consult a flat tree client-side — that
  // would force the whole operation tree to be loaded just for this breadcrumb.
  // Trashed ancestors are filtered out (they render as missing in the chain
  // and break navigation if rendered as breadcrumb links).
  const ancestors = useMemo<AncestorNode[]>(
    () =>
      (doc.ancestors ?? [])
        .filter((a) => !a.isDeleted)
        .map((a) => ({
          id: a.id,
          title: a.title,
          emoji: a.emoji,
          icon: a.icon,
          color: a.color,
        })),
    [doc.ancestors],
  )

  // Only to pick the adaptive icon's glyph (a page with children renders as a
  // folder). Not a fetch of its own: same cache key as the footer's
  // WikiChildDocumentList, which is already listing these children below.
  const { data: childrenData } = useWikiDocumentChildren(operationId, doc.id)
  const hasChildren = (childrenData?.wikiDocumentChildren.length ?? 0) > 0

  function handleTitleBlur() {
    const trimmed = title.trim()
    if (trimmed && trimmed !== doc.title && trimmed.length <= 200) {
      updateDocument.mutate({ id: doc.id, input: { title: trimmed } })
    } else {
      setTitle(doc.title)
    }
    setIsEditingTitle(false)
  }

  function handleIconSelect(next: DocumentIconValue) {
    updateDocument.mutate({
      id: doc.id,
      input: { emoji: next.emoji, icon: next.icon, color: next.color },
    })
  }

  // Split ancestors: first, middle (collapsible), last is the current doc.
  const firstAncestor = ancestors.length > 0 ? ancestors[0] : null
  const middleAncestors = ancestors.length > 2 ? ancestors.slice(1) : []
  const directParent = ancestors.length === 2 ? ancestors[1] : null

  return (
    <div className="flex h-10 items-center gap-1 border-b px-3">
      {/* Document icon — emoji or lucide. hasChildren keeps the adaptive
          default in sync with the tree row (page glyph for leaves, folder
          glyph once children exist). isExpanded stays true here: the user
          is viewing the doc, so its content is "open" by definition.
          Templates and drawings render a fixed, locked glyph instead of the
          picker — the icon can't drift while a doc is one of those. */}
      {doc.isTemplate || doc.kind === "DRAWING" ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <span className="flex size-7 shrink-0 items-center justify-center" />
            }
          >
            <DocumentIcon
              isTemplate={doc.isTemplate}
              isDrawing={doc.kind === "DRAWING"}
              color={doc.color}
              size={18}
            />
          </TooltipTrigger>
          <TooltipContent>
            {doc.isTemplate ? "Template — icon is fixed" : "Drawing — icon is fixed"}
          </TooltipContent>
        </Tooltip>
      ) : (
        <DocumentIconPicker
          value={{ emoji: doc.emoji, icon: doc.icon, color: doc.color }}
          onSelect={handleIconSelect}
          disabled={!isEditor}
          hasChildren={hasChildren}
          isExpanded
        />
      )}

      {/* Breadcrumb: first ancestor */}
      {firstAncestor && (
        <>
          <BreadcrumbSep />
          <BreadcrumbLink node={firstAncestor} />
        </>
      )}

      {/* Breadcrumb: direct parent (when exactly 2 ancestors) */}
      {directParent && (
        <>
          <BreadcrumbSep />
          <BreadcrumbLink node={directParent} />
        </>
      )}

      {/* Breadcrumb: ellipsis popover (when 3+ ancestors) */}
      {middleAncestors.length > 0 && (
        <>
          <BreadcrumbSep />
          <Popover>
            <PopoverTrigger
              render={
                <Button variant="ghost" size="icon-xs" className="text-muted-foreground" />
              }
            >
              <EllipsisIcon className="size-3.5" />
            </PopoverTrigger>
            <PopoverContent align="start" className="w-auto min-w-40 max-w-64 p-1">
              {middleAncestors.map((node) => (
                <Link
                  key={node.id}
                  to={`/wiki/${node.id}`}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
                >
                  {/* Ancestors are parents of the current doc — they all have
                      children by definition, so adaptive resolves to folder. */}
                  <DocumentIcon
                    emoji={node.emoji}
                    icon={node.icon}
                    color={node.color}
                    isDrawing={node.kind === "DRAWING"}
                    hasChildren
                  />
                  <span className="truncate">{node.title}</span>
                </Link>
              ))}
            </PopoverContent>
          </Popover>
        </>
      )}

      {/* Breadcrumb separator before title (when has ancestors) */}
      {ancestors.length > 0 && <BreadcrumbSep />}

      {/* Title (editable) */}
      {isEditingTitle ? (
        <input
          ref={titleRef}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={handleTitleBlur}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur()
            if (e.key === "Escape") {
              setTitle(doc.title)
              setIsEditingTitle(false)
            }
          }}
          autoFocus
          onFocus={(e) => e.target.select()}
          maxLength={200}
          className="min-w-0 flex-1 border-none bg-transparent text-sm font-medium outline-none"
        />
      ) : (
        <button
          className="min-w-0 flex-1 truncate text-left text-sm font-medium"
          onClick={() => {
            if (!isEditor) return
            setTitle(doc.title)
            setIsEditingTitle(true)
          }}
          disabled={!isEditor}
        >
          {doc.title}
        </button>
      )}

      {/* Spacer + right side actions */}
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {/* Who else is here. Renders nothing when you are alone. */}
        <WikiPresenceMenu documentId={doc.id} />

        {/* Outline — the page's headings and checklist prompts as a menu.
            Only offered where there is a text editor to read one from: a
            drawing pane mounts this same header with no editor behind it.
            Read-only safe, like zoom. */}
        {activeEditor && (
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger
                render={
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label="Outline"
                      />
                    }
                  >
                    <ListTreeIcon className="size-4" />
                  </DropdownMenuTrigger>
                }
              />
              <TooltipContent>Outline</TooltipContent>
            </Tooltip>
            {/* max-w so a long heading truncates instead of stretching the
                menu across the pane; the items carry the full text as a
                title attribute. */}
            <DropdownMenuContent align="end" className="w-auto min-w-56 max-w-80">
              <WikiEditorToc editor={activeEditor} />
            </DropdownMenuContent>
          </DropdownMenu>
        )}

        {/* Export — PDF via the print route, Markdown via a modal. Not gated
            on isEditor: exporting is reading. */}
        <WikiExportMenu
          documentId={doc.id}
          title={doc.title}
          isDrawing={doc.kind === "DRAWING"}
        />

        {/* Zoom toggle — not gated on isEditor (focus reading is useful
            without edit rights). */}
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={toggleEditorZoom}
                aria-label={zoomLabel}
                aria-pressed={editorZoomed}
              />
            }
          >
            {editorZoomed ? (
              <Minimize2Icon className="size-4" />
            ) : (
              <Maximize2Icon className="size-4" />
            )}
          </TooltipTrigger>
          <TooltipContent>{zoomLabel}</TooltipContent>
        </Tooltip>

        {/* Backup button */}
        {isEditor && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => openBackupPanel(doc.id)}
                />
              }
            >
              <ClockIcon className="size-4" />
            </TooltipTrigger>
            <TooltipContent>Backup History</TooltipContent>
          </Tooltip>
        )}

        {/* Status badge — only draft or deprecated */}
        <WikiStatusBadge status={doc.status} />

        {/* Read-only badge */}
        {!isEditor && <Badge variant="secondary">Read-only</Badge>}
      </div>
    </div>
  )
}

function BreadcrumbSep() {
  return <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground" />
}

function BreadcrumbLink({ node }: { node: AncestorNode }) {
  return (
    <Link
      to={`/wiki/${node.id}`}
      className="shrink-0 truncate text-sm text-muted-foreground hover:text-foreground"
    >
      {node.title}
    </Link>
  )
}

import { useState } from "react"
import { toast } from "sonner"
import {
  FileDownIcon,
  FileTextIcon,
  PenToolIcon,
  PrinterIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useWikiDrawingScene } from "@/graphql/hooks/wiki"
import { WikiMarkdownExportDialog } from "@/components/wiki/wiki-markdown-export-dialog"
import { markdownFilename } from "@/components/wiki/wiki-markdown-filename"
import { downloadTextFile } from "@/components/wiki/wiki-drawing-download"

interface WikiExportMenuProps {
  documentId: string
  title: string
  /** A drawing has no Markdown body, so that option is not offered for one. */
  isDrawing?: boolean
}

/**
 * The page's export options.
 *
 * PDF leaves through the browser's print dialog, Markdown through a modal,
 * and a drawing's scene straight to a file — three different shapes of
 * interaction, so they sit behind one menu rather than header buttons that
 * look alike and do unlike things.
 *
 * Not gated on edit rights: exporting is reading.
 */
export function WikiExportMenu({
  documentId,
  title,
  isDrawing = false,
}: WikiExportMenuProps) {
  const [markdownOpen, setMarkdownOpen] = useState(false)

  // Fetched only when asked for. The scene carries the bytes of every image
  // on the canvas, which is not something to pull in the background for a
  // menu most people never open.
  const scene = useWikiDrawingScene(documentId, { enabled: false })

  async function downloadScene() {
    try {
      const { data } = await scene.refetch({ throwOnError: true })
      const contents = data?.wikiDrawingScene
      if (!contents) throw new Error("The drawing came back empty.")
      downloadTextFile(
        `${markdownFilename(title)}.excalidraw`,
        contents,
        "application/json;charset=utf-8",
      )
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Could not export this drawing",
      )
    }
  }

  return (
    <>
      <DropdownMenu>
        {/* Icon-only, sized to match the TOC and zoom toggles it sits
            between. The word it used to carry now lives in the tooltip. */}
        <Tooltip>
          <TooltipTrigger
            render={
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="icon-sm" aria-label="Export" />
                }
              >
                <FileDownIcon className="size-4" />
              </DropdownMenuTrigger>
            }
          />
          <TooltipContent>Export</TooltipContent>
        </Tooltip>

        <DropdownMenuContent align="end" className="min-w-44">
          {/* Opens the chromeless print route in a new tab. That page mounts
              the same WikiEditor read-only and calls window.print() once the
              document loads; the user picks "Save as PDF" from the browser's
              dialog. Cookies ride along on the new tab, so the protected
              route still authenticates. */}
          <DropdownMenuItem
            onClick={() =>
              window.open(
                `/wiki/${encodeURIComponent(documentId)}/print`,
                "_blank",
                "noopener,noreferrer",
              )
            }
          >
            <PrinterIcon className="size-4" />
            PDF
          </DropdownMenuItem>

          {/* Offered only for prose. A drawing renders to an empty .md file,
              and an export that silently produces nothing is worse than one
              that is not on the menu. */}
          {!isDrawing && (
            <DropdownMenuItem onClick={() => setMarkdownOpen(true)}>
              <FileTextIcon className="size-4" />
              Markdown
            </DropdownMenuItem>
          )}

          {/* A drawing's counterpart to Markdown: the scene as the
              `.excalidraw` file excalidraw.com and Obsidian's Excalidraw
              plugin open, with the canvas's images inside it. Built by the
              same renderer the wiki export uses, so this file and the one in
              an export zip are the same file.

              Straight to a download rather than through a dialog like
              Markdown's: nobody reads Excalidraw's JSON, so a preview of it
              would be a step between the person and what they asked for.
              Excalidraw's own PNG/SVG export stays on the canvas toolbar,
              which is where somebody wanting a picture rather than an
              editable diagram already looks. */}
          {isDrawing && (
            <DropdownMenuItem
              disabled={scene.isFetching}
              onClick={(event) => {
                // The menu would close and unmount the item mid-fetch.
                event.preventDefault()
                void downloadScene()
              }}
            >
              <PenToolIcon className="size-4" />
              {scene.isFetching ? "Preparing…" : "Excalidraw scene"}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Mounted only while open so closing the dialog drops the rendered
          source rather than holding a stale copy of the body in memory. */}
      {markdownOpen && (
        <WikiMarkdownExportDialog
          documentId={documentId}
          title={title}
          open={markdownOpen}
          onOpenChange={setMarkdownOpen}
        />
      )}
    </>
  )
}

// What the canvas draws over record cards, in HTML rather than in the scene:
// an Open button under the selected card, the full words of the card under
// the pointer, and a marker on a card whose record is gone. None of it
// belongs in the drawing itself. The button and the words are this
// operator's, and the marker depends on who is looking: a host this viewer
// has no access to is not gone for its owner.
//
// Positions come from use-record-cards.ts, which recomputes them as the view
// moves. Only the button takes the pointer; the rest lets clicks through to
// the canvas.

import { ArrowUpRight, Ban, TriangleAlert } from "lucide-react"

import { cn } from "@/lib/utils"
import type { CardRef } from "./drawing-catalogue"
import { REF_LABELS } from "./drawing-records"
import type { RecordOverlayItem } from "./use-record-cards"

interface RecordOverlayProps {
  items: readonly RecordOverlayItem[]
  onOpen: (ref: CardRef) => void
}

export function RecordOverlay({ items, onOpen }: RecordOverlayProps) {
  if (items.length === 0) return null
  return (
    // Above the canvases (z 1-2), below Excalidraw's own UI (z 4), so a
    // panel or menu still covers it.
    <div className="wiki-record-overlay pointer-events-none absolute inset-0 z-[3] overflow-hidden">
      {items.map((item) =>
        item.kind === "hover" ? (
          // The record's words in full: a card with arrows keeps its width,
          // so a long name is cut on the canvas.
          <div
            key={`hover-${item.id}`}
            role="tooltip"
            className="absolute max-w-[min(28rem,90%)] -translate-x-1/2 -translate-y-full rounded-md border bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md"
            style={{ left: item.x + item.width / 2, top: item.y - 8 }}
          >
            <div className="font-medium break-words">{item.title}</div>
            {item.detail && <div className="mt-0.5 break-words text-muted-foreground">{item.detail}</div>}
          </div>
        ) : item.kind === "open" ? (
          <button
            key={`open-${item.id}`}
            type="button"
            onPointerDown={(e) => e.stopPropagation()}
            onClick={() => onOpen(item.ref)}
            className="pointer-events-auto absolute flex -translate-x-1/2 items-center gap-1 rounded-full border bg-popover px-2.5 py-1 text-xs font-medium whitespace-nowrap text-popover-foreground shadow-md hover:bg-accent"
            style={{ left: item.x + item.width / 2, top: item.y + item.height + 10 }}
            title={`Open this ${REF_LABELS[item.ref.kind]} (or Ctrl/⌘-click the card)`}
          >
            Open {REF_LABELS[item.ref.kind]}
            <ArrowUpRight className="size-3.5" />
          </button>
        ) : (
          <div
            key={`${item.kind}-${item.id}`}
            className={cn(
              "absolute rounded-lg border-2 border-dashed",
              item.kind === "missing" ? "border-destructive/70 bg-background/45" : "border-muted-foreground/60 bg-background/35",
            )}
            style={{ left: item.x - 3, top: item.y - 3, width: item.width + 6, height: item.height + 6 }}
          >
            <span
              className={cn(
                "absolute -top-2.5 left-2 flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold whitespace-nowrap shadow-sm",
                item.kind === "missing" ? "bg-destructive text-white" : "bg-muted text-muted-foreground",
              )}
            >
              {item.kind === "missing" ? <TriangleAlert className="size-3" /> : <Ban className="size-3" />}
              {item.kind === "missing" ? `${capitalise(REF_LABELS[item.ref.kind])} deleted` : "No access"}
            </span>
          </div>
        ),
      )}
    </div>
  )
}

function capitalise(word: string): string {
  return word.charAt(0).toUpperCase() + word.slice(1)
}

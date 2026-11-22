import { useState, type CSSProperties, type ReactNode } from "react"
import { EllipsisIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuTrigger } from "@/components/ui/context-menu"
import { dataTableRowClass } from "@/components/ui/virtualized-data-table"
import { cn } from "@/lib/utils"

/**
 * A data-table row with a menu. A click (or Enter) calls onOpen, when there is
 * one, and a double click onDoubleClick (not on a button or link of the row's:
 * theirs); right-clicking anywhere on the row opens the menu at the pointer, and
 * the ⋯ button, given to children to place (usually in the last column), opens
 * the same menu under itself. menu is the ContextMenuItems. A table whose
 * columns change at run time passes its grid in style instead of gridCols.
 */
export function ContextMenuRow({
  gridCols,
  style,
  className,
  onOpen,
  onDoubleClick,
  menu,
  menuLabel,
  menuButtonSize = "icon-sm",
  children,
}: {
  gridCols: string
  /** The ⋯ button's size: icon-xs keeps a one-line row one line; line, a
   *  low row's (py-0.5), as high as a line of its text (20px). */
  menuButtonSize?: "line" | "icon-xs" | "icon-sm"
  style?: CSSProperties
  /** More classes for the row, e.g. a selected one's. */
  className?: string
  /** A click, or Enter; shiftKey says whether shift was down (a range). */
  onOpen?: (e: { shiftKey: boolean }) => void
  /** A double click on the row: its main action where a click does another or none. */
  onDoubleClick?: () => void
  menu: ReactNode
  /** The ⋯ button's accessible name, e.g. "Module actions". */
  menuLabel: string
  children: (menuButton: ReactNode) => ReactNode
}) {
  const [open, setOpen] = useState(false)
  // The ⋯ button the menu last opened from, kept while it closes; null when
  // it opened at the pointer.
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)

  const menuButton = (
    <Button
      variant="ghost"
      size={menuButtonSize === "line" ? "icon-xs" : menuButtonSize}
      // icon-xs is 24px: line takes it down to the row's line, in its place.
      className={menuButtonSize === "line" ? "size-5" : undefined}
      aria-label={menuLabel}
      aria-haspopup="menu"
      aria-expanded={open && anchor !== null}
      // Inside the clickable row: open the menu, not the row.
      onClick={(e) => {
        e.stopPropagation()
        setAnchor(e.currentTarget)
        setOpen(true)
      }}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <EllipsisIcon className="size-4" />
    </Button>
  )

  return (
    <ContextMenu
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        // Only a right click opens it through here: at the pointer. The ⋯
        // button's anchor is kept while it closes, or the closing menu
        // jumped to the page's corner for its fade out.
        if (next) setAnchor(null)
      }}
    >
      <ContextMenuTrigger
        render={
          <div
            className={dataTableRowClass(gridCols, cn(onOpen && "cursor-pointer", className))}
            style={style}
            role={onOpen ? "link" : undefined}
            tabIndex={onOpen ? 0 : undefined}
            onClick={onOpen}
            onDoubleClick={
              onDoubleClick &&
              ((e) => {
                const hit = (e.target as HTMLElement).closest("button, a, input, textarea, select")
                if (hit === null || !e.currentTarget.contains(hit)) onDoubleClick()
              })
            }
            // Shift-click is the row's (a range, where the row has one), not
            // the browser's: it would select every word in between. A double
            // click is the row's too: no word stays selected under it.
            onMouseDown={(e) => {
              if ((onOpen && e.shiftKey) || (onDoubleClick && e.detail > 1)) e.preventDefault()
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") onOpen?.(e)
            }}
          />
        }
      >
        {children(menuButton)}
      </ContextMenuTrigger>
      <ContextMenuContent
        anchor={anchor ?? undefined}
        side={anchor ? "bottom" : undefined}
        align={anchor ? "end" : undefined}
        sideOffset={anchor ? 4 : undefined}
      >
        {menu}
      </ContextMenuContent>
    </ContextMenu>
  )
}

import { useState, type ReactNode } from "react"
import { useNavigate } from "react-router"
import { BanIcon, EllipsisIcon, ShieldCheckIcon, SlidersHorizontalIcon, TrashIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { dataTableRowClass } from "@/components/ui/virtualized-data-table"
import { useModuleStore } from "@/stores/modules"
import { adminActions } from "./module-admin"
import { useUnblockWithToast } from "./use-unblock"

/**
 * A Modules table row and its menu. A click opens the module; right-clicking
 * anywhere on the row opens the menu at the pointer, and the ⋯ button at its
 * end opens the same menu under the button. children renders the cells and
 * is given that button to place.
 */
export function ModuleRow({
  module: m,
  gridCols,
  canAdmin,
  children,
}: {
  module: { instance: string; type: string; status: string }
  gridCols: string
  canAdmin: boolean
  children: (menuButton: ReactNode) => ReactNode
}) {
  const navigate = useNavigate()
  const openDialog = useModuleStore((s) => s.openDialog)
  const { unblock, pending: unblocking } = useUnblockWithToast()
  const [open, setOpen] = useState(false)
  // The ⋯ button while the menu is open from it; null when it opened at the
  // pointer.
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const openDetail = () => navigate(`/modules/${encodeURIComponent(m.instance)}`)

  const menuButton = (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label="Module actions"
      aria-haspopup="menu"
      aria-expanded={open && anchor !== null}
      // Inside the clickable row: open the menu, not the module.
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
        if (!next) setAnchor(null)
      }}
    >
      <ContextMenuTrigger
        render={
          <div
            className={`${dataTableRowClass(gridCols)} cursor-pointer`}
            role="link"
            tabIndex={0}
            onClick={openDetail}
            onKeyDown={(e) => {
              if (e.key === "Enter") openDetail()
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
        <ContextMenuItem onClick={openDetail}>
          <SlidersHorizontalIcon className="size-4" />
          Manage
        </ContextMenuItem>
        {canAdmin && (
          <>
            <ContextMenuSeparator />
            {adminActions(m.status).map((action) =>
              action === "unblock" ? (
                <ContextMenuItem key={action} disabled={unblocking} onClick={() => unblock(m.instance)}>
                  <ShieldCheckIcon className="size-4" />
                  Unblock
                </ContextMenuItem>
              ) : (
                <ContextMenuItem
                  key={action}
                  variant="destructive"
                  onClick={() => openDialog({ kind: action, module: { instance: m.instance, type: m.type } })}
                >
                  {action === "block" ? <BanIcon className="size-4" /> : <TrashIcon className="size-4" />}
                  {action === "block" ? "Block…" : "Delete…"}
                </ContextMenuItem>
              ),
            )}
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  )
}

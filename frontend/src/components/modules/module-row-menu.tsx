import type { ReactNode } from "react"
import { useNavigate } from "react-router"
import { BanIcon, ShieldCheckIcon, SlidersHorizontalIcon, TrashIcon } from "lucide-react"
import { ContextMenuItem, ContextMenuSeparator } from "@/components/ui/context-menu"
import { ContextMenuRow } from "@/components/ui/context-menu-row"
import { useModuleStore } from "@/stores/modules"
import { adminActions } from "./module-admin"
import { useUnblockWithToast } from "./use-unblock"

/**
 * A Modules table row: a click opens the module, and its menu (right-click, or
 * the ⋯ button children places) manages it or changes its state.
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
  const openDetail = () => navigate(`/modules/${encodeURIComponent(m.instance)}`)

  return (
    <ContextMenuRow
      gridCols={gridCols}
      onOpen={openDetail}
      menuLabel="Module actions"
      menu={
        <>
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
        </>
      }
    >
      {children}
    </ContextMenuRow>
  )
}

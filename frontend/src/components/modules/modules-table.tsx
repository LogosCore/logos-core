import { useNavigate } from "react-router"
import { BanIcon, BlocksIcon, EllipsisIcon, ShieldCheckIcon, SlidersHorizontalIcon, TrashIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  VirtualizedDataTable,
  dataTableRowClass,
} from "@/components/ui/virtualized-data-table"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { useAuthStore } from "@/stores/auth"
import { useModuleStore } from "@/stores/modules"
import { Permissions } from "@/constants/permissions"
import { ModuleHealthIndicator } from "@/components/modules/module-health-indicator"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { ModuleIcon } from "@/components/modules/module-icon"
import { adminActions } from "@/components/modules/module-admin"
import { useUnblockWithToast } from "@/components/modules/use-unblock"
import type { ModuleFieldsFragment } from "@/graphql/gql/graphql"

interface ModulesTableProps {
  modules: ModuleFieldsFragment[]
  isLoading: boolean
}

const GRID_COLS = "grid-cols-[1.5fr_1fr_1fr_90px_130px_1fr_1fr_1fr_48px]"

export function ModulesTable({ modules, isLoading }: ModulesTableProps) {
  const hasPermission = useAuthStore((s) => s.hasPermission)
  const openDialog = useModuleStore((s) => s.openDialog)
  const { unblock, pending: unblocking } = useUnblockWithToast()

  const navigate = useNavigate()

  // Blocking, unblocking and deleting share the module:delete permission.
  const canAdmin = hasPermission(Permissions.MODULE_DELETE)
  // The row menu always has "Manage", so the actions column is always there.
  const gridCols = GRID_COLS
  const openDetail = (instance: string) =>
    navigate(`/modules/${encodeURIComponent(instance)}`)

  return (
    <VirtualizedDataTable
      items={modules}
      isLoading={isLoading}
      // The module list is small and unpaginated — there is no next page.
      isFetchingNextPage={false}
      hasNextPage={false}
      fetchNextPage={() => {}}
      gridCols={gridCols}
      entityNoun="modules"
      header={
        <>
          <div>Instance</div>
          <div>Type</div>
          <div>Module</div>
          <div>Version</div>
          <div>Status</div>
          <div>Health</div>
          <div>Last heartbeat</div>
          <div>Registered</div>
          <div />
        </>
      }
      emptyState={
        <>
          <BlocksIcon className="size-8 opacity-50" />
          <p className="text-sm">No modules found.</p>
        </>
      }
      renderRow={(m) => {
        return (
          <div
            className={`${dataTableRowClass(gridCols)} cursor-pointer`}
            role="link"
            tabIndex={0}
            onClick={() => openDetail(m.instance)}
            onKeyDown={(e) => {
              if (e.key === "Enter") openDetail(m.instance)
            }}
          >
            <div className="flex min-w-0 items-center gap-3">
              <ModuleIcon type={m.type} icon={m.icon} className="size-8 text-muted-foreground" />
              <div className="min-w-0">
                <div className="font-medium truncate">{m.instance}</div>
                {m.description && (
                  <div className="text-xs text-muted-foreground truncate">
                    {m.description}
                  </div>
                )}
              </div>
            </div>
            <div>
              <Badge variant="secondary">{m.type}</Badge>
            </div>
            <div className="text-sm truncate">{m.name || "—"}</div>
            <div className="text-sm text-muted-foreground">
              {m.version || "—"}
            </div>
            <div>
              <ModuleStatusIndicator status={m.status} lastHeartbeatAt={m.lastHeartbeatAt} />
            </div>
            <div className="truncate">
              <ModuleHealthIndicator health={m.lastStatus} registered={m.status === "registered"} />
            </div>
            <div className="text-sm text-muted-foreground">
              {m.lastHeartbeatAt ? (
                <FormattedDateTimeText date={m.lastHeartbeatAt} />
              ) : (
                "—"
              )}
            </div>
            <div className="text-sm text-muted-foreground">
              <FormattedDateTimeText date={m.registeredAt} />
            </div>
            {/* The menu sits inside the clickable row: keep its clicks and
                keys from also opening the detail page. */}
            <div
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => e.stopPropagation()}
            >
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={<Button variant="ghost" size="icon-sm" />}
                >
                  <EllipsisIcon className="size-4" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => openDetail(m.instance)}>
                    <SlidersHorizontalIcon className="size-4" />
                    Manage
                  </DropdownMenuItem>
                  {canAdmin && (
                    <>
                      <DropdownMenuSeparator />
                      {adminActions(m.status).map((action) =>
                        action === "unblock" ? (
                          <DropdownMenuItem key={action} disabled={unblocking} onClick={() => unblock(m.instance)}>
                            <ShieldCheckIcon className="size-4" />
                            Unblock
                          </DropdownMenuItem>
                        ) : (
                          <DropdownMenuItem
                            key={action}
                            variant="destructive"
                            onClick={() => openDialog({ kind: action, module: { instance: m.instance, type: m.type } })}
                          >
                            {action === "block" ? <BanIcon className="size-4" /> : <TrashIcon className="size-4" />}
                            {action === "block" ? "Block…" : "Delete…"}
                          </DropdownMenuItem>
                        ),
                      )}
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        )
      }}
    />
  )
}

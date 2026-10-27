import { BlocksIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { VirtualizedDataTable } from "@/components/ui/virtualized-data-table"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { useAuthStore } from "@/stores/auth"
import { Permissions } from "@/constants/permissions"
import { ModuleHealthIndicator } from "@/components/modules/module-health-indicator"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { ModuleIconCell } from "@/components/modules/module-icon"
import { ModuleRow } from "@/components/modules/module-row-menu"
import type { ModuleFieldsFragment } from "@/graphql/gql/graphql"

interface ModulesTableProps {
  modules: ModuleFieldsFragment[]
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  /** A search or filter is on: an empty list says nothing matches. */
  filtered: boolean
}

const GRID_COLS = "grid-cols-[1.5fr_1fr_1fr_90px_130px_1fr_1fr_1fr_48px]"

export function ModulesTable({ modules, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, filtered }: ModulesTableProps) {
  const hasPermission = useAuthStore((s) => s.hasPermission)

  // Blocking, unblocking and deleting share the module:delete permission.
  const canAdmin = hasPermission(Permissions.MODULE_DELETE)
  // The row menu always has "Manage", so the actions column is always there.
  const gridCols = GRID_COLS

  return (
    <VirtualizedDataTable
      items={modules}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
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
        filtered ? (
          <p className="text-sm">No modules match the search and filters.</p>
        ) : (
          <>
            <BlocksIcon className="size-8 opacity-50" />
            <p className="text-sm">No modules have registered yet.</p>
          </>
        )
      }
      renderRow={(m) => (
        <ModuleRow module={m} gridCols={gridCols} canAdmin={canAdmin}>
          {(menuButton) => (
            <>
              <ModuleIconCell
                type={m.type}
                icon={m}
                title={<span className="truncate font-mono font-medium">{m.instance}</span>}
                subtitle={m.description}
                subtitleTitle={m.description ?? undefined}
              />
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
              <div>{menuButton}</div>
            </>
          )}
        </ModuleRow>
      )}
    />
  )
}

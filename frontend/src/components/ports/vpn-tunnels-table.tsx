import type { ReactNode } from "react"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { VpnTunnelFieldsFragment } from "@/graphql/gql/graphql"
import { useRecordModals } from "@/hooks/use-record-modals"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"

export type VPNTunnel = VpnTunnelFieldsFragment

const GRID_COLS = "grid-cols-[minmax(0,1.6fr)_minmax(0,0.8fr)_minmax(0,0.6fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.8fr)]"

/**
 * The operation's VPN tunnels (docs: contracts/vpn-front), the latest
 * opened first: the networks each one routes, its machines connected, its
 * session, who opened it and when, and its streams. A row opens its
 * session.
 */
export function VPNTunnelsTable({
  tunnels,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  emptyState,
}: {
  tunnels: VPNTunnel[]
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  emptyState: ReactNode
}) {
  const modals = useRecordModals()
  return (
    <VirtualizedDataTable
      items={tunnels}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
      gridCols={GRID_COLS}
      entityNoun="VPNs"
      header={
        <>
          <div>Routes</div>
          <div>Machines</div>
          <div>Session</div>
          <div>Opened by</div>
          <div>Since</div>
          <div>Streams</div>
        </>
      }
      emptyState={emptyState}
      renderRow={(v) => (
        <button
          type="button"
          className={dataTableRowClass(GRID_COLS, "cursor-pointer")}
          onClick={() => modals.open({ kind: "session", id: v.sessionId })}
          title={`Open session #${v.sessionNumber}`}
        >
          <span className="truncate font-mono text-sm" title={v.routes.join(", ")}>
            {v.routes.join(", ")}
          </span>
          <span className={cn("text-sm tabular-nums", v.clients > 0 ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground")}>
            {v.listening ? `${v.clients} connected` : "closed"}
          </span>
          <span className="font-mono text-sm">#{v.sessionNumber}</span>
          <span className="truncate">{v.openedByName ?? <span className="text-muted-foreground">someone gone</span>}</span>
          <span className="truncate text-sm tabular-nums">{formatDateTime(v.openedAt)}</span>
          <span className="flex min-w-0 flex-col text-sm tabular-nums">
            <span>
              {v.open}
              <span className="text-muted-foreground"> open · {v.streams} in all</span>
            </span>
            {v.stopsAt && (
              <span className="truncate text-xs text-muted-foreground" title="Core stops it then, unless a stream opens or a machine connects first">
                stops {formatDateTime(v.stopsAt)}
              </span>
            )}
          </span>
        </button>
      )}
    />
  )
}

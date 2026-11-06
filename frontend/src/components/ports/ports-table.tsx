import type { ReactNode } from "react"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { OperationalPortFieldsFragment } from "@/graphql/gql/graphql"
import { useRecordModals } from "@/hooks/use-record-modals"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { frontLabel, hostPort } from "@/components/minion-sessions/tunnel-values"

export type OperationalPort = OperationalPortFieldsFragment

const GRID_COLS = "grid-cols-[minmax(0,1.2fr)_minmax(0,0.6fr)_minmax(0,0.6fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.8fr)]"



/**
 * The operation's operational ports in use, the latest opened first: each
 * one a tunnel task's proxy, with its session, who opened it and when, and
 * its streams. A row opens its session.
 */
export function PortsTable({
  ports,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  emptyState,
}: {
  ports: OperationalPort[]
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  emptyState: ReactNode
}) {
  const modals = useRecordModals()
  return (
    <VirtualizedDataTable
      items={ports}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
      gridCols={GRID_COLS}
      entityNoun="ports"
      header={
        <>
          <div>Port</div>
          <div>Serves</div>
          <div>Session</div>
          <div>Opened by</div>
          <div>Since</div>
          <div>Streams</div>
        </>
      }
      emptyState={emptyState}
      renderRow={(p) => {
        const address = hostPort(p.address ?? window.location.hostname, p.port)
        return (
          <button type="button" className={dataTableRowClass(GRID_COLS, "cursor-pointer")} onClick={() => modals.open({ kind: "session", id: p.sessionId })}
            title={`Open session #${p.sessionNumber}`}>
            <span className="flex min-w-0 flex-col">
              <span className="font-mono text-sm tabular-nums">{p.port}</span>
              <span className="truncate font-mono text-xs text-muted-foreground" title={address}>
                {address}
              </span>
            </span>
            <span className="flex min-w-0 flex-col">
              <span className="truncate" title={p.to ? `to ${p.to}` : undefined}>
                {frontLabel(p.purpose)}
                {p.to && <span className="font-mono text-xs text-muted-foreground"> → {p.to}</span>}
              </span>
              <span className={cn("text-xs", p.listening ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground")}>
                {p.listening ? "listening" : "opening again"}
              </span>
            </span>
            <span className="font-mono text-sm">#{p.sessionNumber}</span>
            <span className="truncate">{p.openedByName ?? <span className="text-muted-foreground">someone gone</span>}</span>
            <span className="truncate text-sm tabular-nums">{formatDateTime(p.openedAt)}</span>
            <span className="text-sm tabular-nums">
              {p.open}
              <span className="text-muted-foreground"> open · {p.streams} in all</span>
            </span>
          </button>
        )
      }}
    />
  )
}

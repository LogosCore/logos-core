import type { ReactNode } from "react"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionListenerFieldsFragment } from "@/graphql/gql/graphql"
import { useRecordModals } from "@/hooks/use-record-modals"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { hostPort } from "@/components/minion-sessions/tunnel-values"

export type MinionListener = MinionListenerFieldsFragment

const GRID_COLS = "grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,0.6fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,0.8fr)]"

/**
 * The operation's reverse port forwards, the latest opened first: the port
 * each one's minion listens on, the destination core connects each of its
 * connections to, its session, who opened it and when, and its streams. A
 * row opens its session.
 */
export function ListenersTable({
  listeners,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  emptyState,
}: {
  listeners: MinionListener[]
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  emptyState: ReactNode
}) {
  const modals = useRecordModals()
  return (
    <VirtualizedDataTable
      items={listeners}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
      gridCols={GRID_COLS}
      entityNoun="ports"
      header={
        <>
          <div>Minion&apos;s port</div>
          <div>Forwards to</div>
          <div>Session</div>
          <div>Opened by</div>
          <div>Since</div>
          <div>Streams</div>
        </>
      }
      emptyState={emptyState}
      renderRow={(l) => {
        const asked = hostPort(l.bind, l.listen)
        const port = l.bound ? l.bound.slice(l.bound.lastIndexOf(":") + 1) : String(l.listen || "any")
        return (
          <button type="button" className={dataTableRowClass(GRID_COLS, "cursor-pointer")} onClick={() => modals.open({ kind: "session", id: l.sessionId })}
            title={`Open session #${l.sessionNumber}`}>
            <span className="flex min-w-0 flex-col">
              <span className="font-mono text-sm tabular-nums">
                {port}
                {l.udp && <span className="font-sans text-xs text-muted-foreground"> UDP</span>}
              </span>
              <span className="truncate font-mono text-xs text-muted-foreground" title={l.bound ?? asked}>
                {l.bound ?? asked}
              </span>
            </span>
            <span className="flex min-w-0 flex-col">
              {l.to ? (
                <span className="truncate font-mono text-sm" title={`core connects to ${l.to}`}>
                  {l.to}
                </span>
              ) : (
                <span className="truncate text-sm" title="A SOCKS5 proxy into core's network, behind the task's password">
                  SOCKS: where each tool asks
                </span>
              )}
              <span className={cn("text-xs", l.listening ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>
                {l.listening ? "listening" : "the minion listens once it has the task"}
              </span>
            </span>
            <span className="font-mono text-sm">#{l.sessionNumber}</span>
            <span className="truncate">{l.openedByName ?? <span className="text-muted-foreground">someone gone</span>}</span>
            <span className="truncate text-sm tabular-nums">{formatDateTime(l.openedAt)}</span>
            <span className="flex min-w-0 flex-col text-sm tabular-nums">
              <span>
                {l.open}
                <span className="text-muted-foreground"> open · {l.streams} in all</span>
              </span>
              {l.stopsAt && (
                <span className="truncate text-xs text-muted-foreground" title="Core stops it then, unless a stream opens first">
                  stops {formatDateTime(l.stopsAt)}
                </span>
              )}
            </span>
          </button>
        )
      }}
    />
  )
}

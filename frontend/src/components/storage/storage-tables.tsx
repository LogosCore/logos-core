import type { ReactNode } from "react"
import { ArchiveIcon, HistoryIcon } from "lucide-react"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { StorageActionFieldsFragment, StorageConsumersQuery } from "@/graphql/gql/graphql"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { actionResult, actionTitle, kindLabel } from "./storage-values"

// The Storage page's lists: what core keeps in a bucket, by operation and
// kind, the largest first; and the maintenance actions run, the latest
// first. Both a page at a time from the server.

export type StorageConsumer = StorageConsumersQuery["storageConsumers"]["edges"][number]["node"]
export type StorageAction = StorageActionFieldsFragment

interface Paging {
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
}

const CONSUMER_COLS = "grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,0.6fr)_minmax(0,0.7fr)]"

/** What core keeps in a bucket: each operation's, of each kind, its objects and bytes. */
export function ConsumersTable({ consumers, emptyState, ...paging }: Paging & { consumers: StorageConsumer[]; emptyState: ReactNode }) {
  return (
    <VirtualizedDataTable
      items={consumers}
      {...paging}
      gridCols={CONSUMER_COLS}
      entityNoun="records"
      header={
        <>
          <div>Operation</div>
          <div>Kind</div>
          <div className="text-right">Objects</div>
          <div className="text-right">Size</div>
        </>
      }
      emptyState={emptyState}
      renderRow={(c) => (
        <div className={dataTableRowClass(CONSUMER_COLS)}>
          <span className="truncate">
            {c.operationId === null ? (
              <span className="text-muted-foreground">no operation&apos;s</span>
            ) : (
              (c.operationName ?? <span className="text-muted-foreground">an operation gone</span>)
            )}
          </span>
          <span className="truncate">{kindLabel(c.purpose, c.kind)}</span>
          <span className="text-right tabular-nums">{c.objects.toLocaleString("en-US")}</span>
          <span className="text-right tabular-nums">{formatBytes(c.bytes)}</span>
        </div>
      )}
    />
  )
}

/** The consumers table's empty state: no record, or none matching. */
export function ConsumersEmpty({ searching }: { searching: boolean }) {
  return (
    <>
      <ArchiveIcon className="size-8 opacity-50" />
      <p className="text-sm">{searching ? "Nothing matches." : "Core keeps nothing here yet."}</p>
    </>
  )
}

const ACTION_COLS = "grid-cols-[minmax(0,1.3fr)_minmax(0,0.7fr)_minmax(0,1fr)_minmax(0,1.6fr)]"

const STATUS_CLASS: Record<string, string> = {
  running: "text-sky-700 dark:text-sky-300",
  succeeded: "text-emerald-700 dark:text-emerald-300",
  failed: "text-red-700 dark:text-red-300",
}

/** The maintenance actions run, the latest first: what, by whom, when, and how it went. */
export function ActionsTable({ actions, ...paging }: Paging & { actions: StorageAction[] }) {
  return (
    <VirtualizedDataTable
      items={actions}
      {...paging}
      gridCols={ACTION_COLS}
      entityNoun="actions"
      header={
        <>
          <div>Action</div>
          <div>By</div>
          <div>Started</div>
          <div>Result</div>
        </>
      }
      emptyState={
        <>
          <HistoryIcon className="size-8 opacity-50" />
          <p className="text-sm">No maintenance has run yet.</p>
        </>
      }
      renderRow={(a) => (
        <div className={dataTableRowClass(ACTION_COLS)}>
          <span className="truncate">{actionTitle(a)}</span>
          <span className="truncate">{a.actor.name ?? a.actor.id}</span>
          <span className="truncate tabular-nums" title={a.finishedAt ? `Ended ${formatDateTime(a.finishedAt)}` : undefined}>
            {formatDateTime(a.startedAt)}
          </span>
          <span className={cn("truncate", STATUS_CLASS[a.status])} title={actionResult(a)}>
            {actionResult(a)}
          </span>
        </div>
      )}
    />
  )
}

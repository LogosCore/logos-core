import type { ReactNode } from "react"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { ModuleIconCell } from "@/components/modules/module-icon"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { platformLabel } from "@/components/builds/build-values"

const GRID_COLS =
  "grid-cols-[minmax(0,1.3fr)_minmax(0,1.1fr)_minmax(0,0.9fr)_minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,0.9fr)_minmax(0,0.9fr)]"

/**
 * An operation's minion sessions, newest first: one per running process of a
 * minion, as its hello described it. Read-only.
 */
export function SessionsTable({
  sessions,
  icons,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  emptyState,
}: {
  sessions: MinionSessionFieldsFragment[]
  icons: ReadonlyMap<string, ModuleIconSvgs>
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  emptyState: ReactNode
}) {
  return (
    <VirtualizedDataTable
      items={sessions}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
      gridCols={GRID_COLS}
      entityNoun="sessions"
      header={
        <>
          <div>Host</div>
          <div>Process</div>
          <div>OS</div>
          <div>Addresses</div>
          <div>Build</div>
          <div>Opened</div>
          <div>Last seen</div>
        </>
      }
      emptyState={emptyState}
      renderRow={(s) => (
        <div className={dataTableRowClass(GRID_COLS)}>
          <div className="flex min-w-0 flex-col">
            <span className="truncate font-medium" title={s.host ?? undefined}>
              {s.host || <span className="text-muted-foreground">unknown host</span>}
            </span>
            <span className="truncate font-mono text-xs text-muted-foreground" title="Session id">
              {s.id}
            </span>
          </div>
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-sm" title={s.proc ?? undefined}>
              {s.proc || <span className="text-muted-foreground">pid</span>} {s.pid}
            </span>
            <span className="truncate text-xs text-muted-foreground" title={userOf(s) || undefined}>
              {[userOf(s), s.integrity].filter(Boolean).join(" · ") || "—"}
            </span>
          </div>
          <div className="flex min-w-0 flex-col">
            <span className="truncate text-sm">{platformLabel(`${s.os}/${s.arch}`)}</span>
            {s.version && <span className="truncate text-xs text-muted-foreground">minion {s.version}</span>}
          </div>
          <div className="truncate font-mono text-xs" title={s.addrs.join("\n") || undefined}>
            {s.addrs.length > 0 ? s.addrs.join(", ") : <span className="text-muted-foreground">—</span>}
          </div>
          <ModuleIconCell
            type="factory"
            icon={icons.get(s.moduleName)}
            title={
              <>
                <span className="truncate font-mono font-medium">{s.moduleName}</span>
                <span className="truncate text-muted-foreground">&nbsp;· {s.target}</span>
              </>
            }
            subtitle={<span>{s.buildDescription || s.buildId}</span>}
            subtitleTitle={`Build ${s.buildId}${s.staged ? "" : ", without staging"}`}
          />
          <div className="truncate text-sm text-muted-foreground">
            <FormattedDateTimeText date={s.createdAt} />
          </div>
          <div className="truncate text-sm text-muted-foreground">
            {s.lastSeenAt ? <FormattedDateTimeText date={s.lastSeenAt} /> : "not yet"}
          </div>
        </div>
      )}
    />
  )
}

/** The user as the OS reports it, with the domain when the minion sent one apart. */
function userOf(s: MinionSessionFieldsFragment): string {
  if (!s.user) return ""
  return s.domain && !s.user.includes("\\") ? `${s.domain}\\${s.user}` : s.user
}

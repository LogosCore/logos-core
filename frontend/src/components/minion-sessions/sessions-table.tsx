import type { ReactNode } from "react"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { KindName } from "@/components/modules/module-icon"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { platformLabel } from "@/components/builds/build-values"

const GRID_COLS =
  "grid-cols-[3rem_minmax(0,1.3fr)_minmax(0,1.1fr)_minmax(0,0.9fr)_minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,0.9fr)_minmax(0,0.9fr)]"

/**
 * An operation's minion sessions, newest first: one per running process of a
 * minion, as its hello described it, by its number in the operation (1, 2,
 * …), one line each. Read-only.
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
          <div>#</div>
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
          {/* The display id operators call the session by. */}
          <div className="truncate font-mono font-medium" title={`Session ${s.number}, ${s.id}`}>
            {s.number}
          </div>
          <div className="truncate font-medium" title={s.host ?? undefined}>
            {s.host || <span className="text-muted-foreground">unknown host</span>}
          </div>
          <div className="truncate text-sm" title={[s.proc, userOf(s)].filter(Boolean).join("\n") || undefined}>
            {s.proc || <span className="text-muted-foreground">pid</span>} {s.pid}
            {(userOf(s) || s.integrity) && (
              <span className="text-muted-foreground"> · {[userOf(s), s.integrity].filter(Boolean).join(" · ")}</span>
            )}
          </div>
          <div className="truncate text-sm">{platformLabel(`${s.os}/${s.arch}`)}</div>
          <div className="truncate font-mono text-xs" title={s.addrs.join("\n") || undefined}>
            {s.addrs.length > 0 ? s.addrs.join(", ") : <span className="text-muted-foreground">—</span>}
          </div>
          <div className="flex min-w-0 items-baseline" title={`Build ${s.buildId}${s.staged ? "" : ", without staging"}`}>
            <KindName type="factory" name={s.moduleName} icon={icons.get(s.moduleName)} className="font-medium" />
            <span className="truncate text-muted-foreground">&nbsp;· {s.target}</span>
          </div>
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

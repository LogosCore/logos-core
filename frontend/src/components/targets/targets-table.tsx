import type { ReactNode } from "react"
import { ExternalLinkIcon, PencilIcon, PlugIcon, RefreshCwIcon, Trash2Icon, UnplugIcon } from "lucide-react"
import { ContextMenuItem, ContextMenuSeparator } from "@/components/ui/context-menu"
import { ContextMenuRow } from "@/components/ui/context-menu-row"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable } from "@/components/ui/virtualized-data-table"
import { useNow } from "@/hooks/use-now"
import { useRecordModals } from "@/hooks/use-record-modals"
import { formatAgo } from "@/components/minion-sessions/sessions-layout"
import { TargetStateBadge } from "./target-state-badge"
import { TARGET_POLICY_LABEL, isPersistent, problemsText, shownState, type Target } from "./target-values"

const GRID_COLS = "grid-cols-[minmax(0,1.8fr)_minmax(0,1.4fr)_minmax(0,0.6fr)_minmax(0,1fr)_48px]"

/**
 * Targets, newest first: where each page is, what the latest call to it came
 * to, its session, and its calls. A row opens its session; its menu calls it
 * now, edits it or removes it.
 */
export function TargetsTable({
  targets,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  canWrite,
  checking,
  onCheck,
  onConnect,
  onEdit,
  onRemove,
  emptyState,
}: {
  targets: Target[]
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  canWrite: boolean
  /** The targets a Check now is waiting on. */
  checking: ReadonlySet<string>
  onCheck: (t: Target) => void
  /** Connect (true) or Disconnect a bind-persistent target. */
  onConnect: (t: Target, connect: boolean) => void
  onEdit: (t: Target) => void
  onRemove: (t: Target) => void
  emptyState: ReactNode
}) {
  const modals = useRecordModals()
  const now = useNow()
  const openSession = (t: Target) => t.sessionId && modals.open({ kind: "session", id: t.sessionId })

  return (
    <VirtualizedDataTable
      items={targets}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
      gridCols={GRID_COLS}
      entityNoun="targets"
      header={
        <>
          <div>Target</div>
          <div>State</div>
          <div>Session</div>
          <div>Last call</div>
          <div />
        </>
      }
      emptyState={emptyState}
      renderRow={(t) => (
        <ContextMenuRow
          gridCols={GRID_COLS}
          onOpen={t.sessionId ? () => openSession(t) : canWrite ? () => onEdit(t) : undefined}
          menuLabel="Target actions"
          menu={
            <>
              {t.sessionId && (
                <ContextMenuItem onClick={() => openSession(t)}>
                  <ExternalLinkIcon className="size-4" />
                  Open session
                </ContextMenuItem>
              )}
              {canWrite && (
                <>
                  {isPersistent(t) ? (
                    t.state === "connected" ? (
                      <ContextMenuItem onClick={() => onConnect(t, false)} disabled={checking.has(t.id)}>
                        <UnplugIcon className="size-4" />
                        Disconnect
                      </ContextMenuItem>
                    ) : (
                      <ContextMenuItem onClick={() => onConnect(t, true)} disabled={checking.has(t.id)}>
                        <PlugIcon className="size-4" />
                        Connect
                      </ContextMenuItem>
                    )
                  ) : (
                    <ContextMenuItem onClick={() => onCheck(t)} disabled={checking.has(t.id)}>
                      <RefreshCwIcon className="size-4" />
                      Check now
                    </ContextMenuItem>
                  )}
                  <ContextMenuItem onClick={() => onEdit(t)}>
                    <PencilIcon className="size-4" />
                    Edit
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem variant="destructive" onClick={() => onRemove(t)}>
                    <Trash2Icon className="size-4" />
                    Remove
                  </ContextMenuItem>
                </>
              )}
            </>
          }
        >
          {(menuButton) => (
            <>
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate font-mono text-sm" title={t.name}>
                  {t.name}
                </span>
                <span className="truncate text-xs text-muted-foreground">
                  Connection {t.conn}
                  {t.connName && <> · {t.connName}</>} · <span className="font-mono">{t.moduleName}</span>
                  {t.policy && <> · {(TARGET_POLICY_LABEL[t.policy] ?? t.policy).toLowerCase()}</>}
                  {!t.retries && <> · no retries</>}
                </span>
              </div>
              <div className="flex min-w-0 flex-col items-start gap-0.5">
                <TargetStateBadge state={shownState(t)} pending={checking.has(t.id)} />
                {t.problems.length > 0 ? (
                  <span className="max-w-full truncate text-xs text-amber-700 dark:text-amber-400" title={problemsText(t)}>
                    {problemsText(t)}
                  </span>
                ) : (t.stateDetail || t.unreachableSince) && (
                  <span className="max-w-full truncate text-xs text-muted-foreground" title={t.stateDetail ?? undefined}>
                    {t.unreachableSince && t.state !== "connected" && <>since {ago(t.unreachableSince, now)}</>}
                    {t.unreachableSince && t.stateDetail && t.state !== "connected" && " · "}
                    {t.stateDetail}
                  </span>
                )}
              </div>
              <div className="truncate text-sm">
                {t.session ? (
                  <span className="font-mono">#{t.session.number}</span>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </div>
              <div className="flex min-w-0 flex-col gap-0.5 text-sm">
                <span className="truncate">{t.lastCallAt ? ago(t.lastCallAt, now) : <span className="text-muted-foreground">never</span>}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {t.calls} {t.calls === 1 ? "call" : "calls"}
                  {t.nextCallAt && <> · next {inFuture(t.nextCallAt, now)}</>}
                </span>
              </div>
              <div>{menuButton}</div>
            </>
          )}
        </ContextMenuRow>
      )}
    />
  )
}

/** How long ago, within a day; older, the date and time. */
function ago(at: string, now: number): ReactNode {
  const text = formatAgo(now - Date.parse(at))
  return text === null ? <FormattedDateTimeText date={at} /> : text
}

/** When a call is due: "in 4m", "now", or the date and time past a day. */
function inFuture(at: string, now: number): ReactNode {
  const ms = Date.parse(at) - now
  if (ms <= 0) return "now"
  const text = formatAgo(ms)
  return text === null ? <FormattedDateTimeText date={at} /> : `in ${text.replace(/ ago$/, "")}`
}

import { useState } from "react"
import { MonitorDotIcon } from "lucide-react"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import {
  useInfiniteMinionSessions,
  useMinionSessionChangedSubscription,
  useMinionSessionsLayout,
} from "@/graphql/hooks/minion-sessions"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { SessionsDatatable } from "@/components/minion-sessions/sessions-datatable"
import { DEFAULT_SPLIT, MAX_SPLIT, MIN_SPLIT } from "@/components/minion-sessions/sessions-layout"
import { SplitPane } from "@/components/ui/split-pane"
import { useCanBuild } from "@/components/builds/build-access"
import { SessionWorkspace } from "@/components/minion-sessions/session-workspace"
import { useSessionTabs } from "@/components/minion-sessions/use-session-tabs"
import { openTab } from "@/components/work-pane/work-pane-tabs"

/**
 * The scoped operation's minion sessions, live: a minion's hello opens one,
 * and its check-ins update it.
 */
export function MinionSessionsPage() {
  const scopedOperation = useScopedOperation()

  usePageMetadata({
    title: scopedOperation ? `Sessions · ${scopedOperation.name}` : "Sessions",
    icon: { kind: "lucide", component: MonitorDotIcon },
  })

  if (!scopedOperation) return <GettingStartedPanel />
  return <MinionSessionsPageInner operationId={scopedOperation.id} />
}

function MinionSessionsPageInner({ operationId }: { operationId: string }) {
  const icons = useFactoryIcons()
  const { canWrite } = useCanBuild(operationId)
  // Columns, sort and filters are the operator's layout, saved in core for
  // all operations; the column filters are the way to narrow the list.
  const { layout, isLoading: layoutLoading, setLayout } = useMinionSessionsLayout()
  // The archived sessions instead of the others: the page's, not the layout's,
  // so it always opens on the live ones.
  const [archived, setArchived] = useState(false)
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useInfiniteMinionSessions(
    { operationId, archived, search: null, filters: layout.filters, sort: layout.sort },
    // The first page waits for the layout, so it is fetched once, as kept.
    { enabled: !layoutLoading },
  )
  useMinionSessionChangedSubscription(operationId)
  const sessions = useConnectionNodes(data, (p) => p.minionSessions)
  const narrowed = layout.filters.length > 0
  // The sessions being worked with, below: an operator's own, in this browser.
  const [tabs, setTabs] = useSessionTabs(operationId)

  return (
    // No padding: the table runs edge to edge, from the sidebar to the
    // window's edge, where the page's padding and a card's border ate space.
    <div className="flex min-h-0 flex-1 flex-col">
      {/* The list above, the session it works with below; the divider
          between them is the operator's, kept with their layout. */}
      <SplitPane
        label="Resize the sessions list and the session below"
        split={layout.split ?? DEFAULT_SPLIT}
        defaultSplit={DEFAULT_SPLIT}
        min={MIN_SPLIT}
        max={MAX_SPLIT}
        onChange={(split) => setLayout({ ...layout, split })}
        top={
          <div className="flex min-h-0 flex-1 flex-col">
            {error ? (
              <div className="m-2 rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
            ) : (
              <SessionsDatatable
                operationId={operationId}
                canWrite={canWrite}
                archived={archived}
                onArchivedChange={setArchived}
                layout={layout}
                onLayoutChange={setLayout}
                onInteract={(s) => setTabs(openTab(tabs, s.id))}
                activeSessionId={tabs.active}
                sessions={sessions}
                icons={icons}
                isLoading={isLoading || layoutLoading}
                isFetchingNextPage={isFetchingNextPage}
                hasNextPage={hasNextPage}
                fetchNextPage={fetchNextPage}
                total={data?.pages[0]?.minionSessions.totalCount ?? 0}
                emptyState={
                  archived && !narrowed ? (
                    <p className="text-sm">No archived sessions.</p>
                  ) : !narrowed ? (
                    <>
                      <MonitorDotIcon className="size-8 opacity-50" />
                      <p className="text-sm">No sessions in this operation yet.</p>
                      <p className="max-w-md text-center text-xs">
                        A minion opens a session when it starts: it says hello through one of its channels with its host and
                        process, and shows up here. Each running process of a build is a session of its own.
                      </p>
                    </>
                  ) : (
                    <p className="text-sm">No sessions match the filters.</p>
                  )
                }
              />
            )}
          </div>
        }
        bottom={<SessionWorkspace tabs={tabs} onTabsChange={setTabs} />}
      />
    </div>
  )
}

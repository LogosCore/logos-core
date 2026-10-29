import { MonitorDotIcon } from "lucide-react"
import { SearchInput } from "@/components/ui/search-input"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import { useInfiniteMinionSessions, useMinionSessionChangedSubscription } from "@/graphql/hooks/minion-sessions"
import { useMinionSessionStore } from "@/stores/minion-sessions"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { SessionsTable } from "@/components/minion-sessions/sessions-table"

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
  // The server searches (see the minionSessions query).
  const search = useMinionSessionStore((s) => s.search)
  const setSearch = useMinionSessionStore((s) => s.setSearch)
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useInfiniteMinionSessions({
    operationId,
    search: search.trim() || null,
  })
  useMinionSessionChangedSubscription(operationId)
  const sessions = useConnectionNodes(data, (p) => p.minionSessions)

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput value={search} onValueChange={setSearch} placeholder="Search sessions or a number..." />
      </div>
      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <SessionsTable
          sessions={sessions}
          icons={icons}
          isLoading={isLoading}
          isFetchingNextPage={isFetchingNextPage}
          hasNextPage={hasNextPage}
          fetchNextPage={fetchNextPage}
          emptyState={
            search.trim() === "" ? (
              <>
                <MonitorDotIcon className="size-8 opacity-50" />
                <p className="text-sm">No sessions in this operation yet.</p>
                <p className="max-w-md text-center text-xs">
                  A minion opens a session when it starts: it says hello through one of its channels with its host and
                  process, and shows up here. Each running process of a build is a session of its own.
                </p>
              </>
            ) : (
              <p className="text-sm">No sessions match the search.</p>
            )
          }
        />
      )}
    </div>
  )
}

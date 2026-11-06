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
  useMinionSession,
  minionSessionKeys,
  useMinionSessionCount,
  useMinionSessionFolders,
  useMinionSessionFolderMutations,
  useSetMinionSessionsFolder,
} from "@/graphql/hooks/minion-sessions"
import { toast } from "sonner"
import { useQueryClient } from "@tanstack/react-query"
import type { MinionSessionQuery } from "@/graphql/gql/graphql"
import { errorMessage } from "@/components/modules/management/api-errors"
import { SessionFolderTabs } from "@/components/minion-sessions/session-folder-tabs"
import { SessionFolderDialog, type FolderDialogTarget } from "@/components/minion-sessions/session-folder-dialog"
import type { FolderControls } from "@/components/minion-sessions/sessions-datatable"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { SessionsDatatable } from "@/components/minion-sessions/sessions-datatable"
import { DEFAULT_SPLIT, MAX_SPLIT, MIN_SPLIT, showFolder } from "@/components/minion-sessions/sessions-layout"
import { SplitPane } from "@/components/ui/split-pane"
import { useCanBuild } from "@/components/builds/build-access"
import { SessionWorkspace } from "@/components/minion-sessions/session-workspace"
import { useSessionTabs } from "@/components/minion-sessions/use-session-tabs"
import { openTab } from "@/components/work-pane/work-pane-tabs"
import { fileBrowserTabId, workspaceTab } from "@/components/minion-sessions/workspace-tabs"

/**
 * The scoped operation's minion sessions, live: a minion's hello opens one,
 * and its check-ins update it.
 */
export function MinionSessionsPage() {
  const scopedOperation = useScopedOperation()
  if (!scopedOperation) return <NoOperation />
  return <MinionSessionsPageInner operationId={scopedOperation.id} operationName={scopedOperation.name} />
}

function NoOperation() {
  usePageMetadata({ title: "Sessions", icon: { kind: "lucide", component: MonitorDotIcon } })
  return <GettingStartedPanel />
}

/** The page's title: the tab in front's session (or its file browser), then the page's. */
function useSessionsTitle(operationName: string, active: string | null) {
  const tab = active ? workspaceTab(active) : null
  const { data } = useMinionSession(tab?.sessionId ?? "")
  const s = tab ? data?.minionSession : null
  const page = `Sessions · ${operationName}`
  const who = s ? [String(s.number), s.host].filter(Boolean).join(" ") : null
  usePageMetadata({
    title: who ? `${tab?.kind === "files" ? `Files ${who}` : who} · ${page}` : page,
    icon: { kind: "lucide", component: MonitorDotIcon },
  })
}

function MinionSessionsPageInner({ operationId, operationName }: { operationId: string; operationName: string }) {
  const icons = useFactoryIcons()
  const queryClient = useQueryClient()
  const { canWrite } = useCanBuild(operationId)
  // Columns, sort and filters are the operator's layout, saved in core for
  // all operations; the column filters are the way to narrow the list.
  const { layout, isLoading: layoutLoading, setLayout } = useMinionSessionsLayout()
  // The archived sessions instead of the others: the page's, not the layout's,
  // so it always opens on the live ones.
  const [archived, setArchived] = useState(false)
  // The operation's folders, and the one shown (the layout's; one gone: All).
  const { data: folderList } = useMinionSessionFolders(operationId)
  const { data: allCount } = useMinionSessionCount(operationId)
  const folderMutations = useMinionSessionFolderMutations(operationId)
  const moveSessions = useSetMinionSessionsFolder()
  const [folderDialog, setFolderDialog] = useState<FolderDialogTarget | null>(null)
  // Sessions to put in the folder New folder makes (its row's Move to folder).
  const [moveIntoNew, setMoveIntoNew] = useState<{ ids: string[] } | null>(null)
  const chosen = layout.folders[operationId] ?? null
  const currentFolder = chosen && folderList?.some((f) => f.id === chosen) ? chosen : null
  const selectFolder = (id: string | null) => setLayout(showFolder(layout, operationId, id))
  const folderControls: FolderControls = {
    list: folderList ?? [],
    current: currentFolder,
    canWrite,
    onSelect: selectFolder,
    onNew: (move) => {
      setMoveIntoNew(move ?? null)
      setFolderDialog({ kind: "new" })
    },
    moveTo: (folderId, which, done) =>
      moveSessions.mutate(
        { operationId, folderId, ...which },
        {
          onSuccess: (d) => {
            const n = d.setMinionSessionsFolder
            const name = folderList?.find((f) => f.id === folderId)?.name ?? "the new folder"
            toast.success(`${folderId ? `Moved ${n} ${n === 1 ? "session" : "sessions"} to ${name}` : `Took ${n} ${n === 1 ? "session" : "sessions"} out of its folder`}`)
            done?.()
          },
          onError: (e) => toast.error(errorMessage(e, "Failed to move the sessions")),
        },
      ),
  }
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useInfiniteMinionSessions(
    { operationId, archived, folderId: currentFolder, search: null, filters: layout.filters, sort: layout.sort },
    // The first page waits for the layout, so it is fetched once, as kept.
    { enabled: !layoutLoading },
  )
  useMinionSessionChangedSubscription(operationId)
  const sessions = useConnectionNodes(data, (p) => p.minionSessions)
  const narrowed = layout.filters.length > 0
  // The sessions being worked with, below: an operator's own, in this browser.
  const [tabs, setTabs] = useSessionTabs(operationId)
  useSessionsTitle(operationName, tabs.active)
  // A tab opened from the list: the pane comes back, parked or not.
  const openShown = (next: typeof tabs) => {
    setTabs(next)
    if (layout.paneParked) setLayout({ ...layout, paneParked: false })
  }

  // Until the layout is in, nothing: drawn with the defaults first, a parked
  // pane showed unparked for a moment (and the divider jumped to its place).
  if (layoutLoading) return <div className="flex min-h-0 flex-1 flex-col" />

  return (
    // No padding: the table runs edge to edge, from the sidebar to the
    // window's edge, where the page's padding and a card's border ate space.
    <div className="flex min-h-0 flex-1 flex-col">
      {/* The list above, the session it works with below; the divider
          between them is the operator's, kept with their layout. */}
      <SplitPane
        label="Resize the sessions list and the session below"
        collapsed={layout.paneParked}
        split={layout.split ?? DEFAULT_SPLIT}
        defaultSplit={DEFAULT_SPLIT}
        min={MIN_SPLIT}
        max={MAX_SPLIT}
        onChange={(split) => setLayout({ ...layout, split })}
        first={
          <div className="flex min-h-0 flex-1 flex-col">
            {error ? (
              <div className="m-2 rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
            ) : (
              <>
              <SessionFolderTabs
                allCount={allCount}
                folders={folderList ?? []}
                current={currentFolder}
                onSelect={selectFolder}
                canWrite={canWrite}
                onNew={() => folderControls.onNew()}
                onEdit={(folder) => setFolderDialog({ kind: "edit", folder })}
                onDelete={(folder) => setFolderDialog({ kind: "delete", folder })}
                onMove={(id, before) => folderMutations.move.mutate({ id, before })}
              />
              <SessionsDatatable
                folders={folderControls}
                operationId={operationId}
                canWrite={canWrite}
                archived={archived}
                onArchivedChange={setArchived}
                layout={layout}
                onLayoutChange={setLayout}
                onInteract={(s) => openShown(openTab(tabs, s.id))}
                onFileBrowser={(s) => openShown(openTab(tabs, fileBrowserTabId(s.id)))}
                activeSessionId={tabs.active ? workspaceTab(tabs.active).sessionId : null}
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
              </>
            )}
          </div>
        }
        second={
          <SessionWorkspace
            tabs={tabs}
            onTabsChange={(next) => {
              setTabs(next)
              // Another tab in front: the table shows its session's folder
              // (All when it is in none). The pane has read its session.
              if (next.active && next.active !== tabs.active) {
                const s = queryClient.getQueryData<MinionSessionQuery>(minionSessionKeys.detail(workspaceTab(next.active).sessionId))?.minionSession
                const folder = s?.folderId ?? null
                if (s && folder !== currentFolder) setLayout(showFolder(layout, operationId, folder))
              }
            }}
            parked={layout.paneParked}
            onParkedChange={(paneParked) => setLayout({ ...layout, paneParked })}
          />
        }
      />
      <SessionFolderDialog
        operationId={operationId}
        target={folderDialog}
        onClose={() => {
          setFolderDialog(null)
          setMoveIntoNew(null)
        }}
        onCreated={(folder) => {
          if (moveIntoNew) folderControls.moveTo(folder.id, moveIntoNew)
          selectFolder(folder.id)
        }}
      />
    </div>
  )
}

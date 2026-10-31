import { useMemo } from "react"
import { FolderTreeIcon, SquareTerminalIcon } from "lucide-react"
import { WorkPane } from "@/components/work-pane/work-pane"
import { openTab, replaceTab, type WorkPaneTabs } from "@/components/work-pane/work-pane-tabs"
import { FileBrowser } from "@/components/file-browser/file-browser"
import { dropFileBrowserState, moveFileBrowserState, setFileBrowserState } from "@/components/file-browser/file-browser-state"
import { ModuleIcon } from "@/components/modules/module-icon"
import { DocumentIcon } from "@/components/wiki/document-icon"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import { useMinionSession, useMinionSessions } from "@/graphql/hooks/minion-sessions"
import { markColorVars, markRowClass } from "./session-mark-colors"
import { SessionFire } from "./session-fire"
import { SessionConsole } from "./session-console"
import { fileBrowserTabId, workspaceTab, WorkspaceActionsContext, type WorkspaceActions } from "./workspace-tabs"

// The lower half of the Sessions page: the sessions an operator works with,
// a tab each ("Interact" in a session's menu opens one), each a console of
// the tasks given its minion (session-console.tsx); and the file browsers
// of their machines ("File Browser" in the menu, file-browser.tsx).

export function SessionWorkspace({ tabs, onTabsChange }: { tabs: WorkPaneTabs; onTabsChange: (next: WorkPaneTabs) => void }) {
  // A session's console and its file browser are one session: read once.
  const sessionIds = [...new Set(tabs.ids.map((id) => workspaceTab(id).sessionId))]
  const loaded = useMinionSessions(sessionIds)
  const sessionOf = (id: string) => loaded[sessionIds.indexOf(workspaceTab(id).sessionId)]
  // A closed file browser's state goes with it.
  const change = (next: WorkPaneTabs) => {
    for (const id of tabs.ids) if (!next.ids.includes(id) && workspaceTab(id).kind === "files") dropFileBrowserState(id)
    onTabsChange(next)
  }
  const actions = useMemo<WorkspaceActions>(
    () => ({
      openConsole: (sessionId) => onTabsChange(openTab(tabs, sessionId)),
      openFileBrowser: (sessionId, path) => {
        const id = fileBrowserTabId(sessionId)
        if (path) setFileBrowserState(id, { path })
        onTabsChange(openTab(tabs, id))
      },
      switchFileBrowser: (from, to) => {
        const fromId = fileBrowserTabId(from)
        const toId = fileBrowserTabId(to)
        // Into a tab already open for it: that one's state stays.
        if (tabs.ids.includes(toId)) dropFileBrowserState(fromId)
        else moveFileBrowserState(fromId, toId)
        onTabsChange(replaceTab(tabs, fromId, toId))
      },
    }),
    [tabs, onTabsChange],
  )
  return (
    <WorkspaceActionsContext value={actions}>
    <WorkPane
      label="Sessions being worked with"
      tabs={tabs.ids.map((id) => {
        // A tab looks as its session's row: the caller's mark, color or style.
        const color = sessionOf(id)?.mark?.color
        return {
          id,
          label: <SessionTabLabel id={id} />,
          className: markRowClass(color),
          style: markColorVars(color),
          backdrop: color === "on-fire" ? <SessionFire /> : undefined,
        }
      })}
      state={tabs}
      onChange={change}
      panel={(id) => <SessionPanel id={id} />}
      empty={
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
          <SquareTerminalIcon className="size-6 opacity-50" />
          <p className="text-sm">&quot;Interact&quot; in a session&apos;s menu opens it here; &quot;File Browser&quot;, its machine&apos;s files.</p>
        </div>
      }
    />
    </WorkspaceActionsContext>
  )
}

/** A tab's label: the factory's icon, the number and the host, and the caller's mark; a file browser's says so. */
function SessionTabLabel({ id }: { id: string }) {
  const icons = useFactoryIcons()
  const tab = workspaceTab(id)
  const { data, isLoading } = useMinionSession(tab.sessionId)
  const s = data?.minionSession
  if (isLoading) return <span className="text-muted-foreground">…</span>
  if (!s) return <span className="text-muted-foreground italic">gone</span>
  const files = tab.kind === "files"
  return (
    <span
      className="flex min-w-0 items-center gap-1.5"
      title={[files ? `Files of session ${s.number}'s machine` : `Session ${s.number}`, s.host, s.user].filter(Boolean).join(" · ")}
    >
      {files ? (
        <FolderTreeIcon className="size-4 shrink-0" />
      ) : (
        <ModuleIcon type="factory" icon={icons.get(s.moduleName)} className="size-4" />
      )}
      <span className="font-mono font-medium">{s.number}</span>
      {s.host && <span className="truncate">{s.host}</span>}
      {(s.mark?.icon || s.mark?.emoji) && <DocumentIcon icon={s.mark.icon} emoji={s.mark.emoji} size={14} />}
    </span>
  )
}

function SessionPanel({ id }: { id: string }) {
  const tab = workspaceTab(id)
  return tab.kind === "files" ? <FileBrowser tabId={id} sessionId={tab.sessionId} /> : <SessionConsole sessionId={tab.sessionId} />
}

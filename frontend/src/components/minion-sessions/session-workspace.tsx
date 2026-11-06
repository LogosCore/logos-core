import { useCallback, useEffect, useMemo } from "react"
import { FeedbackCounts } from "./feedback-parts"
import { AArrowDownIcon, AArrowUpIcon, FolderTreeIcon, LoaderIcon, SquareTerminalIcon, ChevronDownIcon, ChevronUpIcon } from "lucide-react"
import { preloadable, useLoaded, whenIdle } from "@/lib/preloadable"
import { Button } from "@/components/ui/button"
import { WorkPane } from "@/components/work-pane/work-pane"
import { openTab, replaceTab, type WorkPaneTabs } from "@/components/work-pane/work-pane-tabs"
import { dropFileBrowserState, moveFileBrowserState, setFileBrowserState } from "@/components/file-browser/file-browser-state"
import { ModuleIcon } from "@/components/modules/module-icon"
import { MarkIcon } from "./mark-icon"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import { useMinionSession, useMinionSessions, useMinionSessionsLayout } from "@/graphql/hooks/minion-sessions"
import { DEFAULT_PANE_TEXT, PANE_TEXT_SIZES, PaneTextContext, paneTextStyle } from "./pane-text"
import { markColorVars, markRowClass } from "./session-mark-colors"
import { SessionFire } from "./session-fire"
import { fileBrowserTabId, workspaceTab, WorkspaceActionsContext, type WorkspaceActions } from "./workspace-tabs"

// The lower half of the Sessions page: the sessions an operator works with,
// a tab each ("Interact" in a session's menu opens one), each a console of
// the tasks given its minion (session-console.tsx); and the file browsers
// of their machines ("File Browser" in the menu, file-browser.tsx).

export function SessionWorkspace({
  tabs,
  onTabsChange,
  parked,
  onParkedChange,
}: {
  tabs: WorkPaneTabs
  onTabsChange: (next: WorkPaneTabs) => void
  /** Folded to its tab row at the page's bottom (the owner's split folds it). */
  parked: boolean
  onParkedChange: (parked: boolean) => void
}) {
  // Opening a tab, or clicking one, while parked brings the pane back.
  const show = useCallback(() => {
    if (parked) onParkedChange(false)
  }, [parked, onParkedChange])
  // The pane's text size is the operator's, kept with their sessions layout.
  const { layout, setLayout } = useMinionSessionsLayout()
  const textSize = layout.paneTextSize ?? DEFAULT_PANE_TEXT
  const step = (by: -1 | 1) => {
    const next = PANE_TEXT_SIZES[PANE_TEXT_SIZES.indexOf(textSize) + by]
    if (next !== undefined) setLayout({ ...layout, paneTextSize: next })
  }
  // With tabs open, both panels' code once the browser is idle: a console or
  // a file browser opened next then shows at once.
  const hasTabs = tabs.ids.length > 0
  useEffect(() => {
    if (!hasTabs) return
    return whenIdle(() => {
      void consoleCode.load().catch(() => {})
      void filesCode.load().catch(() => {})
    })
  }, [hasTabs])
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
      openConsole: (sessionId) => {
        show()
        onTabsChange(openTab(tabs, sessionId))
      },
      openFileBrowser: (sessionId, path) => {
        const id = fileBrowserTabId(sessionId)
        if (path) setFileBrowserState(id, { path })
        show()
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
    [tabs, onTabsChange, show],
  )
  return (
    <WorkspaceActionsContext value={actions}>
    <PaneTextContext value={textSize}>
    <WorkPane
      label="Sessions being worked with"
      // Dim on the dark theme (index.css, .work-pad): read for hours.
      className="work-pad bg-background text-foreground"
      end={
        // Icons at the tab row's size (its tabs' 16px), the size between them.
        <>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Smaller text"
            title="Smaller text"
            disabled={textSize <= PANE_TEXT_SIZES[0]}
            onClick={() => step(-1)}
          >
            <AArrowDownIcon className="size-4" />
          </Button>
          <span className="w-9 text-center text-xs text-muted-foreground tabular-nums" title="The text size in the tabs">
            {textSize}px
          </span>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Larger text"
            title="Larger text"
            disabled={textSize >= PANE_TEXT_SIZES[PANE_TEXT_SIZES.length - 1]}
            onClick={() => step(1)}
          >
            <AArrowUpIcon className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={parked ? "Bring the pane back" : "Park the pane"}
            title={parked ? "Bring the pane back" : "Park the pane: its tabs alone, at the bottom"}
            aria-expanded={!parked}
            onClick={() => onParkedChange(!parked)}
          >
            {parked ? <ChevronUpIcon className="size-4" /> : <ChevronDownIcon className="size-4" />}
          </Button>
        </>
      }
      parked={parked}
      onTabPress={show}
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
      // The text size is the tabs' contents', not the tab row's.
      panel={(id) => (
        <div className="flex min-h-0 flex-1 flex-col" style={paneTextStyle(textSize)}>
          <SessionPanel id={id} />
        </div>
      )}
      empty={
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
          <SquareTerminalIcon className="size-6 opacity-50" />
          <p className="text-sm">&quot;Interact&quot; in a session&apos;s menu opens it here; &quot;File Browser&quot;, its machine&apos;s files.</p>
        </div>
      }
    />
    </PaneTextContext>
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
      {(s.mark?.icon || s.mark?.emoji) && <MarkIcon icon={s.mark.icon} emoji={s.mark.emoji} size={14} />}
      {!files && <FeedbackCounts errors={s.feedbackErrors} warnings={s.feedbackWarnings} className="text-xs" />}
    </span>
  )
}

// A tab's panel, its code fetched the first time a tab needs it: the console
// and the file browser are more than half the Sessions page's code, so a page
// with no tab open does not wait for them. With tabs open, the kind the open
// tab needs loads first, the other once the browser is idle
// (SessionWorkspace).
const consoleCode = preloadable(() => import("./session-console"))
const filesCode = preloadable(() => import("@/components/file-browser/file-browser"))

function SessionPanel({ id }: { id: string }) {
  const tab = workspaceTab(id)
  return tab.kind === "files" ? <FilesPanel tabId={id} sessionId={tab.sessionId} /> : <ConsolePanel sessionId={tab.sessionId} />
}

function ConsolePanel({ sessionId }: { sessionId: string }) {
  const code = useLoaded(consoleCode)
  return code ? <code.SessionConsole sessionId={sessionId} /> : <PanelLoading />
}

function FilesPanel({ tabId, sessionId }: { tabId: string; sessionId: string }) {
  const code = useLoaded(filesCode)
  return code ? <code.FileBrowser tabId={tabId} sessionId={sessionId} /> : <PanelLoading />
}

function PanelLoading() {
  return (
    <div className="flex flex-1 items-center justify-center text-muted-foreground">
      <LoaderIcon className="size-4 animate-spin" />
    </div>
  )
}

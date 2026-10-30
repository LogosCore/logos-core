import { SquareTerminalIcon } from "lucide-react"
import { WorkPane } from "@/components/work-pane/work-pane"
import type { WorkPaneTabs } from "@/components/work-pane/work-pane-tabs"
import { ModuleIcon } from "@/components/modules/module-icon"
import { DocumentIcon } from "@/components/wiki/document-icon"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"

// The lower half of the Sessions page: the sessions an operator works with,
// a tab each ("Interact" in a session's menu opens one). What a tab will
// hold, a session's commands and their output, is not built yet.

export function SessionWorkspace({ tabs, onTabsChange }: { tabs: WorkPaneTabs; onTabsChange: (next: WorkPaneTabs) => void }) {
  return (
    <WorkPane
      label="Sessions being worked with"
      tabs={tabs.ids.map((id) => ({ id, label: <SessionTabLabel id={id} /> }))}
      state={tabs}
      onChange={onTabsChange}
      panel={(id) => <SessionPanel id={id} />}
      empty={
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
          <SquareTerminalIcon className="size-6 opacity-50" />
          <p className="text-sm">&quot;Interact&quot; in a session&apos;s menu opens it here.</p>
        </div>
      }
    />
  )
}

/** A tab's label: the factory's icon, the number and the host, and the caller's mark. */
function SessionTabLabel({ id }: { id: string }) {
  const icons = useFactoryIcons()
  const { data, isLoading } = useMinionSession(id)
  const s = data?.minionSession
  if (isLoading) return <span className="text-muted-foreground">…</span>
  if (!s) return <span className="text-muted-foreground italic">gone</span>
  return (
    <span className="flex min-w-0 items-center gap-1.5" title={[`Session ${s.number}`, s.host, s.user].filter(Boolean).join(" · ")}>
      <ModuleIcon type="factory" icon={icons.get(s.moduleName)} className="size-4" />
      <span className="font-mono font-medium">{s.number}</span>
      {s.host && <span className="truncate">{s.host}</span>}
      {(s.mark?.icon || s.mark?.emoji) && <DocumentIcon icon={s.mark.icon} emoji={s.mark.emoji} size={14} />}
    </span>
  )
}

function SessionPanel({ id }: { id: string }) {
  const { data } = useMinionSession(id)
  const s = data?.minionSession
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
      <SquareTerminalIcon className="size-6 opacity-50" />
      <p className="text-sm">{s ? `Working with session ${s.number} comes here.` : "This session does not exist any more."}</p>
    </div>
  )
}

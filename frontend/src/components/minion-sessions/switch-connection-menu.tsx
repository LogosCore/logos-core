import { ArrowRightLeftIcon, CheckIcon } from "lucide-react"
import { ContextMenuItem, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger } from "@/components/ui/context-menu"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { choiceDetail } from "./connection-choices"
import { useSessionConnectionChoices, useSwitchConnection } from "./use-connection-choices"

// Switch connection in a session row's menu: the session's connections, the
// one in use checked; a pick queues the session's switch_connection command,
// as the session's Connections card does. Shown only for a build with more
// than one connection; what it lists is read when it opens.

export function SwitchConnectionMenu({ s, canWrite }: { s: MinionSessionFieldsFragment; canWrite: boolean }) {
  if (s.connCount < 2) return null
  const why = !canWrite ? "Needs operator in this operation" : s.state !== "OPEN" ? "The session is closed" : undefined
  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger disabled={why !== undefined} title={why}>
        <ArrowRightLeftIcon />
        Switch connection
      </ContextMenuSubTrigger>
      <ContextMenuSubContent className="min-w-56">
        <Connections s={s} />
      </ContextMenuSubContent>
    </ContextMenuSub>
  )
}

function Connections({ s }: { s: MinionSessionFieldsFragment }) {
  const connections = useSessionConnectionChoices(s.id)
  const { command, loading, pending, switchTo } = useSwitchConnection(s.id)
  if (connections.length === 0 || loading) {
    return <ContextMenuItem disabled>Loading…</ContextMenuItem>
  }
  if (!command) {
    return <ContextMenuItem disabled>Built without a command to switch</ContextMenuItem>
  }
  return connections.map((c) => (
    <ContextMenuItem
      key={c.place}
      disabled={c.inUse || pending}
      title={c.inUse ? "In use" : `Queue ${command.id}: the minion moves once this connection answers`}
      onClick={() => void switchTo(c.place, connections)}
    >
      {c.inUse ? <CheckIcon /> : <span className="size-4" />}
      <span className="flex min-w-0 flex-col">
        <span className="font-mono">{c.name}</span>
        <span className="truncate text-xs text-muted-foreground">{choiceDetail(c)}</span>
      </span>
    </ContextMenuItem>
  ))
}

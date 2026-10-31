import type { ReactNode } from "react"
import { ContextMenuItem, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger } from "@/components/ui/context-menu"

// An action on an item of a result handler's view, in the item's context
// menu: run one of the session's commands of a handler on it (List
// directory runs a dir_listing command on a folder). With one such command
// it is an item; with several, a submenu of them, the view's own command
// first (handler-commands.ts, commandsFor). None: no item. Every view's
// actions are made of these.

export interface MenuCommand {
  id: string
  label: string
}

export function HandlerActionItem({
  label,
  icon,
  commands,
  onRun,
}: {
  label: string
  icon?: ReactNode
  /** The commands it can run, the first the one to prefer. */
  commands: readonly MenuCommand[]
  onRun: (commandId: string) => void
}) {
  if (commands.length === 0) return null
  if (commands.length === 1) {
    const c = commands[0]
    return (
      <ContextMenuItem onClick={() => onRun(c.id)}>
        {icon}
        {label}
        <span className="ml-auto pl-4 font-mono text-xs text-muted-foreground">{c.id}</span>
      </ContextMenuItem>
    )
  }
  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        {icon}
        {label}
      </ContextMenuSubTrigger>
      <ContextMenuSubContent>
        {commands.map((c) => (
          <ContextMenuItem key={c.id} onClick={() => onRun(c.id)}>
            <span className="truncate">{c.label}</span>
            <span className="ml-auto pl-4 font-mono text-xs text-muted-foreground">{c.id}</span>
          </ContextMenuItem>
        ))}
      </ContextMenuSubContent>
    </ContextMenuSub>
  )
}

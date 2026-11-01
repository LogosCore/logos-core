import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { MenuCommand } from "./action-menu"

// One of core's actions in a view's tool row (Upload here, Walk from here):
// with one command for it, a click runs it; with several, a menu of them,
// as the items' context menus offer. None: no button.

export function ActionButton({
  icon,
  label,
  title,
  commands,
  onRun,
  disabled = false,
}: {
  icon: ReactNode
  /** Its accessible name. */
  label: string
  /** Its hover: what it does, where. */
  title: string
  commands: readonly MenuCommand[]
  onRun: (commandId: string) => void
  disabled?: boolean
}) {
  if (commands.length === 0) return null
  if (commands.length === 1)
    return (
      <Button variant="ghost" size="icon-xs" aria-label={label} title={`${title} (${commands[0].id})`} disabled={disabled} onClick={() => onRun(commands[0].id)}>
        {icon}
      </Button>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        render={
          <Button variant="ghost" size="icon-xs" aria-label={label} title={title}>
            {icon}
          </Button>
        }
      />
      {/* As wide as its labels: the content is the trigger's width otherwise, an icon's. */}
      <DropdownMenuContent align="end" className="w-auto min-w-48">
        {commands.map((c) => (
          <DropdownMenuItem key={c.id} onClick={() => onRun(c.id)}>
            <span className="whitespace-nowrap">{c.label || c.id}</span>
            <span className="ml-auto pl-4 font-mono text-xs text-muted-foreground">{c.id}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

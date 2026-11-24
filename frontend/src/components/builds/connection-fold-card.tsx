import type { ReactNode } from "react"
import { ChevronRightIcon } from "lucide-react"
import { TableBody } from "@/components/ui/table"
import { KIND_FLOWS } from "@/components/endpoints/connection-kind-flows"
import { ModuleIcon } from "@/components/modules/module-icon"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { cn } from "@/lib/utils"

/**
 * One connection as a card that folds (a build's, a session's): its head
 * always shows what it is (its place, its name, a build's, else its
 * channel, and its kind; a session's connections have no names), a badge
 * ("in use"), its actions at the end, and lines under it (where it connects,
 * a note, a target); unfolded, its rows (the settings set, the factory's
 * values) as an InfoCard's table.
 */
export function ConnectionFoldCard({
  place,
  name,
  moduleName,
  kind,
  icon,
  badge,
  actions,
  lines,
  expanded,
  onToggle,
  children,
}: {
  place: number
  /** A build's connection's name; a session's have none, and show their channel. */
  name?: string
  moduleName: string
  kind: string
  icon?: ModuleIconSvgs
  badge?: ReactNode
  /** At the head's end: buttons. */
  actions?: ReactNode
  /** Under the head, folded or not. */
  lines?: ReactNode
  expanded: boolean
  onToggle: () => void
  /** The table's rows, shown unfolded. */
  children: ReactNode
}) {
  return (
    <section className="min-w-0 rounded-xl border bg-card" aria-label={`Connection ${place}: ${name ?? moduleName}`}>
      <div className="flex flex-col gap-1 px-4 py-2">
        <div className="flex min-h-7 items-center gap-2">
          <button
            type="button"
            className="flex min-w-0 flex-1 items-baseline gap-2 text-left"
            onClick={onToggle}
            aria-expanded={expanded}
            title={expanded ? "Fold" : "Show its settings"}
          >
            <ChevronRightIcon className={cn("size-4 shrink-0 self-center text-muted-foreground transition-transform", expanded && "rotate-90")} />
            <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{place}.</span>
            <ModuleIcon type="channel" icon={icon} className="size-4 shrink-0 self-center" />
            <span className="truncate font-mono text-sm font-semibold">{name ?? moduleName}</span>
            <span className="shrink-0 text-xs text-muted-foreground" title={`${moduleName} · ${kind}`}>
              {KIND_FLOWS[kind]?.label ?? kind}
            </span>
            {badge}
          </button>
          {actions}
        </div>
        {lines && <div className="flex flex-col gap-1 pl-6 empty:hidden">{lines}</div>}
      </div>
      {expanded && (
        <table className="w-full table-fixed border-t text-sm">
          <colgroup>
            <col className="w-40" />
            <col />
          </colgroup>
          <TableBody>{children}</TableBody>
        </table>
      )}
    </section>
  )
}


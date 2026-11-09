import { useCallback, useEffect, useState } from "react"
import { Virtuoso } from "react-virtuoso"
import { LoaderIcon, SearchIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { useInfiniteDelegateSessions } from "@/graphql/hooks/minion-targets"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { cn } from "@/lib/utils"

/** A session a target's calls and opens go through: a delegate. */
export interface DelegateSession {
  id: string
  number: number
  host?: string | null
  user?: string | null
}

/** "#12 · ws01 · CORP\\svc", as a delegate is shown. */
function delegateLabel(s: DelegateSession): string {
  return [`#${s.number}`, s.host, s.user].filter(Boolean).join(" · ")
}

/**
 * Picks a target's delegate (docs: contracts/minion-messages/delegation): one
 * of the operation's sessions whose build has a delegate command serving the
 * target's channel kind, searched by the server; or none, an instance of the
 * channel then carrying the target (not offered where only delegates carry
 * the kind).
 */
export function DelegatePicker({
  id,
  operationId,
  moduleName,
  kind,
  version,
  value,
  onChange,
  required,
  disabled,
  invalid,
}: {
  id: string
  operationId: string
  moduleName: string
  kind: string
  version: number
  value: DelegateSession | null
  onChange: (s: DelegateSession | null) => void
  /** Only delegates carry the kind: no "none". */
  required: boolean
  disabled?: boolean
  invalid?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [debounced, setDebounced] = useState("")
  useEffect(() => {
    const t = setTimeout(() => setDebounced(search.trim()), 180)
    return () => clearTimeout(t)
  }, [search])
  const { data, isLoading, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteDelegateSessions(
    { operationId, moduleName, kind, version, search: debounced || null },
    open,
  )
  const sessions = useConnectionNodes(data, (p) => p.minionSessions)
  const focus = useCallback((node: HTMLInputElement | null) => node?.focus(), [])

  function pick(s: DelegateSession | null) {
    onChange(s)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            id={id}
            type="button"
            variant="outline"
            disabled={disabled}
            aria-invalid={invalid || undefined}
            className={cn("w-full justify-start font-normal", !value && "text-muted-foreground")}
          >
            <span className="truncate">
              {value ? delegateLabel(value) : required ? "Pick a delegate" : "None: an instance of the channel"}
            </span>
          </Button>
        }
      />
      <PopoverContent className="w-(--anchor-width)! min-w-72 overflow-hidden p-1.5" side="bottom" align="start">
        <div className="relative">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={focus}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search sessions..."
            className="h-7 pl-7 text-xs"
          />
        </div>
        {!required && (
          <button
            type="button"
            className={cn("mt-1 flex w-full rounded-md px-1.5 py-1.5 text-left text-sm hover:bg-accent/50", !value && "bg-accent/40")}
            onClick={() => pick(null)}
          >
            None: an instance of the channel
          </button>
        )}
        {isLoading && (
          <div className="flex items-center justify-center py-4 text-muted-foreground">
            <LoaderIcon className="size-3.5 animate-spin" />
          </div>
        )}
        {!isLoading && sessions.length === 0 && (
          <div className="py-4 text-center text-sm text-muted-foreground">
            {debounced
              ? "No session matches."
              : `No session's minion serves ${moduleName} ${kind} connections: its build needs a delegate command that does.`}
          </div>
        )}
        {!isLoading && sessions.length > 0 && (
          <Virtuoso
            data={sessions}
            style={{ height: `${Math.min(sessions.length, 8) * 34}px` }}
            endReached={() => {
              if (hasNextPage && !isFetchingNextPage) void fetchNextPage()
            }}
            itemContent={(_, s) => (
              <button
                key={s.id}
                type="button"
                aria-selected={value?.id === s.id}
                className={cn(
                  "flex w-full items-baseline gap-2 rounded-md px-1.5 py-1.5 text-left text-sm hover:bg-accent/50",
                  value?.id === s.id && "bg-accent/40",
                )}
                onClick={() => pick({ id: s.id, number: s.number, host: s.host, user: s.user })}
              >
                <span className="font-mono">#{s.number}</span>
                <span className="min-w-0 flex-1 truncate">{[s.host, s.user].filter(Boolean).join(" · ")}</span>
                <span className="shrink-0 font-mono text-xs text-muted-foreground">{s.moduleName}</span>
              </button>
            )}
          />
        )}
      </PopoverContent>
    </Popover>
  )
}

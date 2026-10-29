import { useId, useRef, useState, type ReactNode, type RefObject } from "react"
import { SearchIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ModuleIcon } from "@/components/modules/module-icon"
import { useInfiniteChannelEndpoints } from "@/graphql/hooks/channel-endpoints"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useDebounced } from "@/hooks/use-debounced"
import { cn } from "@/lib/utils"
import type { EndpointOption } from "./connection-card"
import { settingsSummary, type ChannelKind } from "./build-values"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"

/** A channel a blank connection can go through, or why it cannot. */
export interface BlankChannel {
  name: string
  why: string | null
}

/**
 * Picks what a connection starts from: one of the operation's endpoints of
 * the offered channel kinds, which the server searches by name, description,
 * channel or settings and sends a page at a time as the list scrolls; or
 * (blank) nothing but a channel. Arrow keys and Enter pick from the list
 * without the mouse.
 */
export function ConnectionPickerDialog({
  open,
  onClose,
  title,
  description,
  operationId,
  kinds,
  channels,
  channelIcons,
  blank = [],
  onEndpoint,
  onBlank,
}: {
  open: boolean
  onClose: () => void
  title: string
  description: ReactNode
  operationId: string
  /** The channel kinds whose endpoints are offered. */
  kinds: readonly string[]
  channels: ReadonlyMap<string, ChannelKind>
  channelIcons: ReadonlyMap<string, ModuleIconSvgs>
  /** Channels to start a blank connection through; none offers no blank one. */
  blank?: readonly BlankChannel[]
  onEndpoint: (endpoint: EndpointOption) => void
  onBlank?: (moduleName: string) => void
}) {
  const search = useRef<HTMLInputElement>(null)
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl" initialFocus={search}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {/* Keyed so each opening starts with an empty search. */}
        {open && (
          <PickerBody
            key="picker"
            search={search}
            operationId={operationId}
            kinds={kinds}
            blank={blank.length > 0}
            channels={channels}
            channelIcons={channelIcons}
            onEndpoint={onEndpoint}
          />
        )}
        <DialogFooter className="sm:items-center">
          {blank.length > 0 && (
            <div className="flex flex-wrap items-center gap-2 sm:mr-auto">
              <span className="text-sm text-muted-foreground">Start blank:</span>
              {blank.map((c) => (
                <Button
                  key={c.name}
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={c.why !== null}
                  title={c.why ?? `A ${c.name} connection with nothing filled in`}
                  onClick={() => onBlank?.(c.name)}
                >
                  <ModuleIcon type="channel" icon={channelIcons.get(c.name)} />
                  <span className="font-mono">{c.name}</span>
                </Button>
              ))}
            </div>
          )}
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PickerBody({
  search,
  operationId,
  kinds,
  blank,
  channels,
  channelIcons,
  onEndpoint,
}: {
  search: RefObject<HTMLInputElement | null>
  operationId: string
  kinds: readonly string[]
  /** A blank connection is offered too. */
  blank: boolean
  channels: ReadonlyMap<string, ChannelKind>
  channelIcons: ReadonlyMap<string, ModuleIconSvgs>
  onEndpoint: (endpoint: EndpointOption) => void
}) {
  const listId = useId()
  const [query, setQuery] = useState("")
  const debounced = useDebounced(query.trim(), 300)
  const [active, setActive] = useState(0)
  const { data, isLoading, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteChannelEndpoints(
    { operationId, search: debounced || null, moduleNames: [...kinds] },
    { enabled: kinds.length > 0 },
  )
  const matches = useConnectionNodes(data, (p) => p.channelEndpoints)
  const total = data?.pages[0]?.channelEndpoints.totalCount ?? 0
  const current = Math.min(active, matches.length - 1)

  if (kinds.length === 0 || (!isLoading && debounced === "" && total === 0)) {
    return (
      <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
        No {kinds.join(" or ")} endpoints in this operation yet. {blank ? "Start a blank connection below, or make" : "Make"}{" "}
        endpoints on the Endpoints page to reuse their settings.
      </p>
    )
  }

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="relative">
        <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          ref={search}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault()
              const next = Math.max(0, Math.min(matches.length - 1, current + (e.key === "ArrowDown" ? 1 : -1)))
              setActive(next)
              document.getElementById(`${listId}-${next}`)?.scrollIntoView({ block: "nearest" })
            } else if (e.key === "Enter") {
              e.preventDefault()
              if (current >= 0) onEndpoint(matches[current])
            }
          }}
          placeholder="Search by name, description or settings…"
          title='Wrap in "quotes" to match whole words'
          aria-label="Search endpoints"
          role="combobox"
          aria-expanded
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={current >= 0 ? `${listId}-${current}` : undefined}
          autoComplete="off"
          spellCheck={false}
          className="pl-9"
        />
      </div>
      <div
        id={listId}
        role="listbox"
        aria-label="Endpoints"
        className="max-h-[min(24rem,50vh)] overflow-y-auto rounded-lg border p-1"
        onScroll={(e) => {
          const el = e.currentTarget
          if (hasNextPage && !isFetchingNextPage && el.scrollTop + el.clientHeight >= el.scrollHeight - 32) {
            void fetchNextPage()
          }
        }}
      >
        {isLoading && matches.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted-foreground">Loading…</p>}
        {!isLoading && matches.length === 0 && (
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">No endpoint matches.</p>
        )}
        {matches.map((ep, i) => {
          const summary = settingsSummary(channels.get(ep.moduleName), ep.values)
          return (
            <div
              key={ep.id}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === current}
              className={cn(
                "flex cursor-pointer items-start gap-3 rounded-md px-2.5 py-2",
                i === current && "bg-accent text-accent-foreground",
              )}
              onMouseMove={() => i !== current && setActive(i)}
              onClick={() => onEndpoint(ep)}
            >
              <ModuleIcon type="channel" icon={channelIcons.get(ep.moduleName)} className="mt-0.5 size-5" />
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <div className="flex min-w-0 items-baseline gap-2">
                  <span className="truncate text-sm font-medium">{ep.name}</span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">{ep.moduleName}</span>
                </div>
                {ep.description && <span className="truncate text-xs text-muted-foreground">{ep.description}</span>}
                {summary && <span className="truncate font-mono text-xs text-muted-foreground">{summary}</span>}
                {ep.problems.length > 0 && (
                  <span className="text-xs text-amber-700 dark:text-amber-400">
                    Doesn't fit the channel's current settings: check them after picking
                  </span>
                )}
              </div>
            </div>
          )
        })}
        {isFetchingNextPage && <p className="px-3 py-2 text-center text-xs text-muted-foreground">Loading more…</p>}
      </div>
      <p className="text-xs text-muted-foreground">
        {total} {total === 1 ? "endpoint" : "endpoints"}
        {debounced !== "" && " match"}
        {" · "}↑↓ to move, Enter to pick
      </p>
    </div>
  )
}

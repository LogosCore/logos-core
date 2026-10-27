import { useId, useMemo, useRef, useState, type ReactNode, type RefObject } from "react"
import { SearchIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ModuleIcon } from "@/components/modules/module-icon"
import { cn } from "@/lib/utils"
import type { EndpointOption } from "./connection-card"
import { endpointSearchText, matchesQuery, settingsSummary, type ChannelKind } from "./build-values"

/** A channel a blank connection can go through, or why it cannot. */
export interface BlankChannel {
  name: string
  why: string | null
}

/**
 * Picks what a connection starts from: one of the operation's endpoints,
 * found by name, description, channel or settings, or (blank) nothing but a
 * channel. Arrow keys and Enter pick from the list without the mouse.
 */
export function ConnectionPickerDialog({
  open,
  onClose,
  title,
  description,
  kinds,
  endpoints,
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
  /** The channel kinds offered, for the empty list. */
  kinds: readonly string[]
  /** The endpoints to offer, in the order given. */
  endpoints: readonly EndpointOption[]
  channels: ReadonlyMap<string, ChannelKind>
  channelIcons: ReadonlyMap<string, string>
  /** Channels to start a blank connection through; none offers no blank one. */
  blank?: readonly BlankChannel[]
  onEndpoint: (endpoint: EndpointOption) => void
  onBlank?: (moduleName: string) => void
}) {
  const search = useRef<HTMLInputElement>(null)
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl" initialFocus={endpoints.length > 0 ? search : true}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {/* Keyed so each opening starts with an empty search. */}
        {open && (
          <PickerBody
            key="picker"
            search={search}
            kinds={kinds}
            blank={blank.length > 0}
            endpoints={endpoints}
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
  kinds,
  blank,
  endpoints,
  channels,
  channelIcons,
  onEndpoint,
}: {
  search: RefObject<HTMLInputElement | null>
  kinds: readonly string[]
  /** A blank connection is offered too. */
  blank: boolean
  endpoints: readonly EndpointOption[]
  channels: ReadonlyMap<string, ChannelKind>
  channelIcons: ReadonlyMap<string, string>
  onEndpoint: (endpoint: EndpointOption) => void
}) {
  const listId = useId()
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  const indexed = useMemo(
    () =>
      endpoints.map((ep) => ({
        ep,
        text: endpointSearchText(ep, channels.get(ep.moduleName)),
        summary: settingsSummary(channels.get(ep.moduleName), ep.values),
      })),
    [endpoints, channels],
  )
  const matches = indexed.filter((x) => matchesQuery(x.text, query))
  const current = Math.min(active, matches.length - 1)

  if (endpoints.length === 0) {
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
              if (current >= 0) onEndpoint(matches[current].ep)
            }
          }}
          placeholder="Search by name, description or settings…"
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
      <div id={listId} role="listbox" aria-label="Endpoints" className="max-h-[min(24rem,50vh)] overflow-y-auto rounded-lg border p-1">
        {matches.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted-foreground">No endpoint matches.</p>}
        {matches.map(({ ep, summary }, i) => (
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
                <span className="truncate font-mono text-sm font-medium">{ep.name}</span>
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
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        {matches.length === endpoints.length
          ? `${endpoints.length} ${endpoints.length === 1 ? "endpoint" : "endpoints"}`
          : `${matches.length} of ${endpoints.length} endpoints`}
        {" · "}↑↓ to move, Enter to pick
      </p>
    </div>
  )
}

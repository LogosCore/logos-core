import { useEffect, useMemo, useRef, useState, useLayoutEffect } from "react"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ArrowDownToLineIcon,
  DownloadIcon,
  EraserIcon,
  PauseIcon,
  PlayIcon,
  ScrollTextIcon,
  TriangleAlertIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useSubscription } from "@/hooks/use-subscription"
import {
  ModuleLogsDocument,
  type ModuleDetailFieldsFragment,
  type ModuleLogLevel,
} from "@/graphql/gql/graphql"
import { cn } from "@/lib/utils"
import {
  applyEvent,
  emptyBuffer,
  formatAttrs,
  formatTime,
  lineMatches,
  lineText,
  type LogBuffer,
  type LogLine,
} from "./log-buffer"
import { LogDetailPane } from "./log-detail-pane"

const LEVELS: { value: ModuleLogLevel; label: string }[] = [
  { value: "DEBUG", label: "Debug" },
  { value: "INFO", label: "Info" },
  { value: "WARN", label: "Warn" },
  { value: "ERROR", label: "Error" },
]

const LEVEL_CLASS: Record<ModuleLogLevel, string> = {
  DEBUG: "text-muted-foreground",
  INFO: "text-sky-700 dark:text-sky-400",
  WARN: "text-amber-700 dark:text-amber-400",
  ERROR: "text-destructive",
}

interface LogsTabProps {
  module: ModuleDetailFieldsFragment
  canRead: boolean
}

/**
 * Live tail of the module's logs. The subscription only runs while this tab
 * is mounted (and the browser tab visible); core relays records from the
 * module and stores nothing, so what is shown lives only in this buffer.
 */
export function LogsTab({ module, canRead }: LogsTabProps) {
  const [level, setLevel] = useState<ModuleLogLevel>("INFO")
  const [buf, setBuf] = useState<LogBuffer>(emptyBuffer)
  const [search, setSearch] = useState("")
  // Last line id shown while paused; null when live.
  const [pausedAt, setPausedAt] = useState<number | null>(null)
  const [follow, setFollow] = useState(true)
  // The line shown in the detail pane; it closes once the line leaves the
  // buffer (cleared, capped out, level changed).
  const [selectedId, setSelectedId] = useState<number | null>(null)
  // A rejection is tied to the registration it happened in: a module that
  // registers again is worth another try.
  const [rejected, setRejected] = useState<{ message: string; registeredAt: string } | null>(null)

  const registered = module.status === "registered"
  const error = rejected && rejected.registeredAt === module.registeredAt ? rejected.message : null

  useSubscription(
    ModuleLogsDocument,
    { instance: module.instance, minLevel: level },
    {
      enabled: canRead && registered && !error,
      onData: (data) => setBuf((b) => applyEvent(b, data.moduleLogs)),
      onError: (message) => setRejected({ message, registeredAt: module.registeredAt }),
    },
  )

  const shown = useMemo(() => {
    const live = pausedAt === null ? buf.lines : buf.lines.filter((l) => l.id <= pausedAt)
    return search ? live.filter((l) => lineMatches(l, search)) : live
  }, [buf.lines, pausedAt, search])
  const pending = pausedAt === null ? 0 : buf.lines.filter((l) => l.id > pausedAt).length
  const selected = selectedId === null ? undefined : buf.lines.find((l) => l.id === selectedId)

  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: shown.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 22,
    overscan: 20,
  })

  useLayoutEffect(() => {
    if (follow && shown.length > 0) {
      virtualizer.scrollToIndex(shown.length - 1, { align: "end" })
    }
  }, [follow, shown.length, virtualizer])

  // Esc closes the pane; ↑/↓ move the selection through the shown lines.
  // Ignored while typing in a field.
  useEffect(() => {
    if (selectedId === null) return
    function onKey(e: KeyboardEvent) {
      const t = e.target as HTMLElement | null
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return
      if (e.key === "Escape") {
        setSelectedId(null)
        return
      }
      if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return
      const i = shown.findIndex((l) => l.id === selectedId)
      if (i < 0) return
      const next = i + (e.key === "ArrowDown" ? 1 : -1)
      if (next < 0 || next >= shown.length) return
      e.preventDefault()
      setFollow(false)
      setSelectedId(shown[next].id)
      virtualizer.scrollToIndex(next, { align: "auto" })
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [selectedId, shown, virtualizer])

  if (!canRead) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        Viewing live logs requires module:logs:read.
      </p>
    )
  }

  function changeLevel(next: ModuleLogLevel) {
    // A new level is a new stream with its own backlog; start clean.
    setLevel(next)
    setBuf(emptyBuffer)
    setPausedAt(null)
    setFollow(true)
  }

  function download() {
    const text = shown.map(lineText).join("\n") + "\n"
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }))
    const a = document.createElement("a")
    a.href = url
    a.download = `${module.instance}-${new Date().toISOString().replace(/[:.]/g, "-")}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          value={search}
          onValueChange={setSearch}
          placeholder="Filter lines..."
          debounceMs={100}
          title=""
          className="relative w-full max-w-sm"
        />
        <Select value={level} onValueChange={(v) => v && changeLevel(v as ModuleLogLevel)}>
          <SelectTrigger className="w-32" aria-label="Minimum level">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {LEVELS.map((l) => (
              <SelectItem key={l.value} value={l.value}>
                {l.label}+
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant="outline"
          onClick={() => setPausedAt(pausedAt === null ? (buf.lines.at(-1)?.id ?? 0) : null)}
        >
          {pausedAt === null ? <PauseIcon /> : <PlayIcon />}
          {pausedAt === null ? "Pause" : pending > 0 ? `Resume (${pending} new)` : "Resume"}
        </Button>
        <Button
          variant="outline"
          onClick={() => {
            setBuf((b) => ({ ...b, lines: [] }))
            setPausedAt(null)
          }}
          disabled={buf.lines.length === 0}
        >
          <EraserIcon />
          Clear
        </Button>
        <Button variant="outline" onClick={download} disabled={shown.length === 0}>
          <DownloadIcon />
          Download
        </Button>
        <StreamStatus
          buf={buf}
          registered={registered}
          error={error}
          className="ml-auto"
        />
      </div>

      {(buf.dropped > 0 || buf.lost > 0) && (
        <div className="flex items-center gap-2 rounded-md bg-amber-500/10 px-3 py-1.5 text-xs text-amber-800 dark:text-amber-300">
          <TriangleAlertIcon className="size-3.5 shrink-0" />
          {buf.dropped > 0 && <span>{buf.dropped.toLocaleString()} lines dropped (rate limit or slow connection).</span>}
          {buf.lost > 0 && <span>Some batches were lost in transit.</span>}
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
        <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
          <div
            ref={scrollRef}
            onScroll={(e) => {
              const el = e.currentTarget
              setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 32)
            }}
            className="min-h-80 flex-1 overflow-auto rounded-xl border bg-card font-mono text-xs"
          >
            {shown.length === 0 ? (
              <EmptyState hasLines={buf.lines.length > 0} streaming={buf.state === "STREAMING"} />
            ) : (
              <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
                {virtualizer.getVirtualItems().map((item) => (
                  <LogRow
                    key={shown[item.index].id}
                    line={shown[item.index]}
                    index={item.index}
                    measure={virtualizer.measureElement}
                    top={item.start}
                    selected={shown[item.index].id === selectedId}
                    onSelect={() =>
                      setSelectedId((id) => (id === shown[item.index].id ? null : shown[item.index].id))
                    }
                  />
                ))}
              </div>
            )}
          </div>
          {!follow && shown.length > 0 && (
            <Button
              size="sm"
              className="absolute bottom-3 right-5 shadow-md"
              onClick={() => setFollow(true)}
            >
              <ArrowDownToLineIcon />
              Latest
            </Button>
          )}
        </div>
        {selected && (
          <LogDetailPane
            line={selected}
            onClose={() => setSelectedId(null)}
            className="max-lg:max-h-[50vh] lg:w-[28rem] lg:shrink-0"
          />
        )}
      </div>
    </div>
  )
}

function LogRow({
  line,
  index,
  top,
  measure,
  selected,
  onSelect,
}: {
  line: LogLine
  index: number
  top: number
  measure: (el: Element | null) => void
  selected: boolean
  onSelect: () => void
}) {
  const attrs = formatAttrs(line.attrs)
  return (
    <div
      ref={measure}
      data-index={index}
      role="button"
      tabIndex={-1}
      aria-pressed={selected}
      onClick={onSelect}
      className={cn(
        "absolute left-0 flex w-full cursor-pointer gap-3 border-l-2 px-3 py-0.5",
        selected ? "border-primary bg-accent" : "border-transparent hover:bg-muted/50",
      )}
      style={{ transform: `translateY(${top}px)` }}
    >
      <span className="shrink-0 tabular-nums text-muted-foreground" title={line.time}>
        {formatTime(line.ms)}
      </span>
      <span className={cn("w-10 shrink-0 font-semibold", LEVEL_CLASS[line.level])}>{line.level}</span>
      <span className="min-w-0 whitespace-pre-wrap break-words">
        {line.message}
        {attrs && <span className="text-muted-foreground"> {attrs}</span>}
      </span>
    </div>
  )
}

function StreamStatus({
  buf,
  registered,
  error,
  className,
}: {
  buf: LogBuffer
  registered: boolean
  error: string | null
  className?: string
}) {
  let dot = "bg-muted-foreground"
  let text = "Connecting…"
  if (error) {
    dot = "bg-destructive"
    text = error
  } else if (!registered) {
    text = "Module not registered — resumes when it is back"
  } else if (buf.state === "STREAMING") {
    dot = "bg-green-600 animate-pulse"
    text = "Live"
  } else if (buf.state === "DISCONNECTED") {
    dot = "bg-amber-500"
    text = "Module unreachable — retrying"
  } else if (buf.state === "ENDED") {
    dot = "bg-amber-500"
    text = `Stream ended${buf.reason ? ` (${buf.reason.replace(/_/g, " ")})` : ""} — reconnecting`
  }
  return (
    <span className={cn("inline-flex items-center gap-2 text-sm text-muted-foreground", className)}>
      <span className={cn("size-2 rounded-full", dot)} aria-hidden />
      {text}
    </span>
  )
}

function EmptyState({ hasLines, streaming }: { hasLines: boolean; streaming: boolean }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 font-sans text-muted-foreground">
      <ScrollTextIcon className="size-8 opacity-50" />
      <p className="text-sm">
        {hasLines ? "No lines match the filter." : streaming ? "Waiting for log lines…" : "No log lines yet."}
      </p>
    </div>
  )
}

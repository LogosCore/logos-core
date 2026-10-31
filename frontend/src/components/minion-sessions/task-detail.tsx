import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import {
  ChevronDownIcon,
  ChevronUpIcon,
  CopyIcon,
  DownloadIcon,
  FileIcon,
  FolderIcon,
  RotateCcwIcon,
  SearchIcon,
  SquareIcon,
  SquareTerminalIcon,
  WrapTextIcon,
  XIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { useMinionTaskOutput } from "@/graphql/hooks/minion-tasks"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { lineOf, type CommandDef } from "./command-line"
import { findMatches, MAX_MATCHES, segments } from "./output-find"
import { FINISHED, useTaskActions, type Task } from "./task-actions"
import { StateLabel, TaskTimes } from "./task-state"
import { formatDateTime } from "@/lib/date-format"

// The right of a session's console: the task picked in the list, whole
// (its command is the list's row): where it stands and who gave it, what can be done with it,
// and what came back: the output as text, read again as more comes (Ctrl+F
// finds in it), and any data the command gave (an ls as a table, the rest
// as JSON). The raw bytes download as they came.

const API_URL = import.meta.env.VITE_API_URL as string

const WRAP_KEY = "logos.console.wrap"

function readWrap(): boolean {
  try {
    return localStorage.getItem(WRAP_KEY) !== "off"
  } catch {
    return true
  }
}

function saveWrap(wrap: boolean) {
  try {
    localStorage.setItem(WRAP_KEY, wrap ? "on" : "off")
  } catch {
    // Kept for the page only.
  }
}

export function TaskDetail({
  task: t,
  command,
  canWrite,
  onQueued,
  findSignal,
}: {
  task: Task
  command?: CommandDef
  canWrite: boolean
  onQueued: (t: Task) => void
  /** Goes up when Ctrl+F is pressed in the console: the find bar opens. */
  findSignal: number
}) {
  const line = lineOf(t.command, t.args as Record<string, unknown> | null, command)
  const actions = useTaskActions(t, line, { canWrite, onQueued })
  // Read again as parts come and when it ends.
  const outputQ = useMinionTaskOutput(t.id, `${t.parts}:${t.state}`, FINISHED.has(t.state))
  const detail = outputQ.data?.minionTask
  const output = detail?.output ?? ""
  const data = (detail?.data as Record<string, unknown> | null | undefined) ?? null
  // Whether it gave data, guessed from its parts until they are read (parts
  // with no output are data), so an ls opens on its Data tab at once rather
  // than on an empty Output first.
  const hasData = detail ? data !== null : t.outputSize === 0 && t.parts > 0

  const [tab, setTab] = useState<"output" | "data" | null>(null)
  // Data alone shows as data; with output, the output first.
  const shown = tab === "data" && hasData ? "data" : tab === "output" || !hasData || t.outputSize > 0 ? "output" : "data"
  const [wrap, setWrap] = useState(readWrap)

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      {/* One line: the tabs, where it stands, what can be done. The command
          itself is the list's picked row. */}
      <div className="flex items-center gap-2 border-b px-2 py-1">
        <div className="flex shrink-0 items-center gap-1">
          <TabButton active={shown === "output"} onClick={() => setTab("output")}>
            Output
          </TabButton>
          {hasData && (
            <TabButton active={shown === "data"} onClick={() => setTab("data")}>
              Data
            </TabButton>
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-0.5 border-l pl-2">
          <StateLabel t={t} />
          <span className="text-xs text-muted-foreground" title={`Queued ${formatAbsolute(t.queuedAt)}`}>
            by {t.issuedByName ?? "someone gone"}
          </span>
          <TaskTimes t={t} />
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          {shown === "output" && (
            <Button variant={wrap ? "secondary" : "ghost"} size="icon-xs" title={wrap ? "Lines wrap" : "Lines do not wrap"} aria-label="Wrap lines" aria-pressed={wrap}
              onClick={() => {
                saveWrap(!wrap)
                setWrap(!wrap)
              }}>
              <WrapTextIcon />
            </Button>
          )}
          <Button variant="ghost" size="icon-xs" title="Copy the output" aria-label="Copy the output" disabled={!actions.hasOutput}
            onClick={() => void actions.copyOutput(detail ? output : undefined)}>
            <CopyIcon />
          </Button>
          <Button variant="ghost" size="icon-xs" title="Download the output as it came, bytes and all" aria-label="Download the raw output"
            disabled={!actions.hasOutput}
            render={actions.hasOutput ? <a href={`${API_URL}/minion-tasks/${encodeURIComponent(t.id)}/output`} download /> : undefined}>
            <DownloadIcon />
          </Button>
          {actions.canRunAgain && (
            <Button variant="ghost" size="icon-xs" title="Run it again" aria-label="Run the task again" disabled={actions.runningAgain} onClick={actions.runAgain}>
              <RotateCcwIcon />
            </Button>
          )}
          {actions.canStop && (
            <Button variant="ghost" size="icon-xs" title="Stop it" aria-label="Stop the task" disabled={actions.stopping} onClick={actions.stop}>
              <SquareIcon />
            </Button>
          )}
        </div>
      </div>
      {t.err && <div className="border-b px-3 py-1 font-mono text-xs whitespace-pre-wrap text-red-600 dark:text-red-400">{t.err}</div>}

      {shown === "output" ? (
        <OutputView
          output={output}
          loading={outputQ.isLoading}
          running={!FINISHED.has(t.state)}
          empty={t.state === "QUEUED" || t.state === "SENT" ? "No output yet." : FINISHED.has(t.state) ? "No output." : "Waiting for output…"}
          wrap={wrap}
          findSignal={findSignal}
          notes={[
            detail && !detail.outputUtf8 ? "Not all of it is UTF-8: bytes that are not show as �; the download has them." : null,
            t.outputTruncated ? "Cut at 4 MiB: the rest was dropped." : null,
          ]}
        />
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-2">{data && <DataView data={data} />}</div>
      )}
    </div>
  )
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "cursor-pointer rounded px-2 py-0.5 text-xs font-medium",
        active ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  )
}

function OutputView({
  output,
  loading,
  running,
  empty,
  wrap,
  findSignal,
  notes,
}: {
  output: string
  loading: boolean
  running: boolean
  empty: string
  wrap: boolean
  findSignal: number
  notes: (string | null)[]
}) {
  const box = useRef<HTMLDivElement>(null)
  // A running task's output stays at the bottom as it grows, unless the
  // operator scrolled up to read; a finished one opens at the top.
  const atBottom = useRef(running)
  useLayoutEffect(() => {
    const el = box.current
    if (el && atBottom.current) el.scrollTop = el.scrollHeight
  }, [output])

  // Find: opened by Ctrl+F, closed by Escape.
  const [finding, setFinding] = useState(false)
  const [query, setQuery] = useState("")
  const [current, setCurrent] = useState(0)
  const findInput = useRef<HTMLInputElement>(null)
  const firstSignal = useRef(findSignal)
  useEffect(() => {
    if (findSignal === firstSignal.current) return
    setFinding(true)
    // After the bar renders.
    requestAnimationFrame(() => {
      findInput.current?.focus()
      findInput.current?.select()
    })
  }, [findSignal])

  const matches = useMemo(() => (finding ? findMatches(output, query) : []), [finding, output, query])
  const at = matches.length > 0 ? Math.min(current, matches.length - 1) : -1
  const parts = useMemo(() => (matches.length > 0 ? segments(output, matches, query.length) : null), [output, matches, query.length])

  useEffect(() => {
    if (at < 0) return
    // The output alone scrolls, to put the match in its middle:
    // scrollIntoView would move the page too.
    const el = box.current
    const mark = el?.querySelector<HTMLElement>("[data-current]")
    if (el && mark) {
      const r = mark.getBoundingClientRect()
      const b = el.getBoundingClientRect()
      el.scrollTop += r.top - b.top - (el.clientHeight - r.height) / 2
      if (r.left < b.left || r.right > b.right) el.scrollLeft += r.left - b.left - (el.clientWidth - r.width) / 2
    }
    atBottom.current = false
  }, [at, query])

  const step = (by: 1 | -1) => {
    if (matches.length === 0) return
    setCurrent((at + by + matches.length) % matches.length)
  }
  const onFindKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault()
      step(e.shiftKey ? -1 : 1)
    } else if (e.key === "Escape") {
      e.preventDefault()
      setFinding(false)
    }
  }

  const shownNotes = notes.filter((n): n is string => n !== null)
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {finding && (
        <div className="absolute top-2 right-4 z-10 flex items-center gap-1 rounded-md border bg-popover px-2 py-1 shadow-md">
          <SearchIcon className="size-3.5 text-muted-foreground" />
          <input
            ref={findInput}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setCurrent(0)
            }}
            onKeyDown={onFindKey}
            placeholder="Find in output"
            aria-label="Find in the output"
            spellCheck={false}
            className="w-40 bg-transparent text-xs outline-none"
          />
          <span className="min-w-14 text-right text-xs text-muted-foreground tabular-nums">
            {query === "" ? "" : matches.length === 0 ? "none" : `${at + 1}/${matches.length}${matches.length >= MAX_MATCHES ? "+" : ""}`}
          </span>
          <Button variant="ghost" size="icon-xs" aria-label="Previous match" title="Previous (Shift+Enter)" disabled={matches.length === 0} onClick={() => step(-1)}>
            <ChevronUpIcon />
          </Button>
          <Button variant="ghost" size="icon-xs" aria-label="Next match" title="Next (Enter)" disabled={matches.length === 0} onClick={() => step(1)}>
            <ChevronDownIcon />
          </Button>
          <Button variant="ghost" size="icon-xs" aria-label="Close find" title="Close (Escape)" onClick={() => setFinding(false)}>
            <XIcon />
          </Button>
        </div>
      )}
      <div
        ref={box}
        tabIndex={0}
        aria-label="Output"
        onScroll={(e) => {
          const el = e.currentTarget
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}
        className="min-h-0 flex-1 overflow-auto outline-none"
      >
        {output === "" ? (
          // Nothing while it is read: a placeholder for a moment is a flicker.
          !loading && (
            <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
              <SquareTerminalIcon className="size-6 opacity-50" />
              <p className="text-sm">{empty}</p>
            </div>
          )
        ) : (
          <pre className={cn("px-3 py-2 font-mono text-xs", wrap ? "break-all whitespace-pre-wrap" : "w-max whitespace-pre")}>
            {parts
              ? parts.map((s, i) =>
                  s.match === undefined ? (
                    s.text
                  ) : (
                    <mark
                      key={i}
                      data-current={s.match === at ? "" : undefined}
                      className={cn("rounded-sm text-inherit", s.match === at ? "bg-amber-400/80 dark:bg-amber-500/70" : "bg-amber-200/70 dark:bg-amber-400/30")}
                    >
                      {s.text}
                    </mark>
                  ),
                )
              : output}
          </pre>
        )}
      </div>
      {shownNotes.length > 0 && <p className="border-t px-3 py-1 text-xs text-muted-foreground">{shownNotes.join(" ")}</p>}
    </div>
  )
}

interface Entry {
  name: string
  dir?: boolean
  size?: number
  mode?: string
  modified?: string
}

function isEntries(v: unknown): v is Entry[] {
  return Array.isArray(v) && v.every((e) => typeof e === "object" && e !== null && typeof (e as Entry).name === "string")
}

/** A byte count, short: "4.2 KiB". */
function size(n?: number): string {
  if (n === undefined) return ""
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let v = n
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return u === 0 ? `${v} B` : `${v.toFixed(1)} ${units[u]}`
}

/** What a command gave besides output: an ls's entries as a table, the rest as JSON. */
function DataView({ data }: { data: Record<string, unknown> }) {
  const [all, setAll] = useState(false)
  const { entries, path, ...rest } = data
  if (!isEntries(entries)) return <Json value={data} />
  // Directories first, then by name.
  const sorted = [...entries].sort((a, b) => Number(!!b.dir) - Number(!!a.dir) || a.name.localeCompare(b.name))
  const shown = all ? sorted : sorted.slice(0, 500)
  return (
    <div className="overflow-hidden rounded-md border">
      {typeof path === "string" && (
        <div className="border-b bg-muted/40 px-3 py-1 font-mono text-xs text-muted-foreground">
          {path} · {entries.length} {entries.length === 1 ? "entry" : "entries"}
        </div>
      )}
      <table className="w-full text-xs">
        <tbody>
          {shown.map((e) => (
            <tr key={e.name} className="border-b last:border-b-0 hover:bg-muted/40">
              <td className="w-full max-w-0 truncate px-3 py-1 font-mono" title={e.name}>
                <span className="inline-flex items-baseline gap-1.5">
                  {e.dir ? (
                    <FolderIcon className="size-3.5 self-center text-amber-600 dark:text-amber-400" />
                  ) : (
                    <FileIcon className="size-3.5 self-center text-muted-foreground" />
                  )}
                  {e.name}
                </span>
              </td>
              <td className="px-3 py-1 text-right whitespace-nowrap text-muted-foreground tabular-nums">{e.dir ? "" : size(e.size)}</td>
              <td className="px-3 py-1 font-mono whitespace-nowrap text-muted-foreground">{e.mode}</td>
              <td className="px-3 py-1 whitespace-nowrap text-muted-foreground" title={e.modified ? formatAbsolute(e.modified) : undefined}>
                {e.modified ? formatDateTime(e.modified) : ""}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {!all && sorted.length > shown.length && (
        <button type="button" className="w-full cursor-pointer border-t px-3 py-1 text-xs text-muted-foreground hover:bg-muted/40" onClick={() => setAll(true)}>
          Show all {sorted.length}
        </button>
      )}
      {Object.keys(rest).length > 0 && <Json value={rest} />}
    </div>
  )
}

function Json({ value }: { value: unknown }) {
  return <pre className="px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap">{JSON.stringify(value, null, 2)}</pre>
}

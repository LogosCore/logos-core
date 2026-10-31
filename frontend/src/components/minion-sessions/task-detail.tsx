import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import {
  BracesIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CopyIcon,
  DownloadIcon,
  RotateCcwIcon,
  SearchIcon,
  SquareIcon,
  SquareTerminalIcon,
  WrapTextIcon,
  XIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import { useMinionTaskDocument, useMinionTaskOutput } from "@/graphql/hooks/minion-tasks"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { lineOf, type CommandDef } from "./command-line"
import { readDirListing } from "./dir-listing"
import { DirListingView } from "./dir-listing-view"
import { findMatches, MAX_MATCHES, segments } from "./output-find"
import { FINISHED, useTaskActions, type Task } from "./task-actions"
import { StateLabel, TaskTimes } from "./task-state"

// The right of a session's console: the task picked in the list, whole
// (its command is the list's row): where it stands and who gave it, what can be done with it,
// and what came back: the output as text, read again as more comes (Ctrl+F
// finds in it), and any data the command gave, in its result handler's
// view (a dir_listing as a file browser) or as raw JSON. Output and data
// download as the minion sent them.
// The JSON tab shows the task as core keeps it, for debugging.

const API_URL = import.meta.env.VITE_API_URL as string

// The viewer's toggles, kept in this browser: whether output lines wrap,
// whether data shows as raw JSON.
const WRAP_KEY = "logos.console.wrap"
const RAW_DATA_KEY = "logos.console.rawData"

function readFlag(key: string, fallback: boolean): boolean {
  try {
    const v = localStorage.getItem(key)
    return v === null ? fallback : v === "on"
  } catch {
    return fallback
  }
}

function saveFlag(key: string, on: boolean) {
  try {
    localStorage.setItem(key, on ? "on" : "off")
  } catch {
    // Kept for the page only.
  }
}

const noChange = () => {}

export function TaskDetail({
  task: t,
  command,
  canWrite,
  onQueued,
  onOpenPath,
  findSignal,
}: {
  task: Task
  command?: CommandDef
  canWrite: boolean
  onQueued: (t: Task) => void
  /** Runs the task's command again on another path (a folder opened in a
   *  listing); absent where the viewer may not give tasks. */
  onOpenPath?: (path: string) => void
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
  // Whether it gave data, known from its size until it is read (and
  // guessed for a task kept before that: parts with no output are data),
  // so an ls opens on its Data tab at once rather than on an empty Output.
  const hasData = detail ? data !== null : t.dataSize > 0 || (t.outputSize === 0 && t.parts > 0)
  // The handler's view, for data that fits it; the rest shows raw.
  const listing = t.handler === "dir_listing" && !t.dataProblem ? readDirListing(data) : null
  const dataNotes = [
    t.dataProblem ? `The data does not fit ${t.handler}: ${t.dataProblem}. It shows raw.` : null,
    t.handler && t.handler !== "dir_listing" ? `Its handler, ${t.handler}, is not one this version shows: the data shows raw.` : null,
    t.missingParts > 0
      ? `${t.missingParts} ${t.missingParts === 1 ? "part" : "parts"} before the last never came: the data may lack some.`
      : null,
    t.dataTruncated ? "Cut at 4 MiB with the output: the rest was dropped." : null,
  ].filter((n): n is string => n !== null)

  const [tab, setTab] = useState<"output" | "data" | "json" | null>(null)
  // Data alone shows as data; with output, the output first.
  const shown =
    tab === "json"
      ? "json"
      : tab === "data" && hasData
        ? "data"
        : tab === "output" || !hasData || t.outputSize > 0
          ? "output"
          : "data"
  const [wrap, setWrap] = useState(() => readFlag(WRAP_KEY, true))
  const [rawData, setRawData] = useState(() => readFlag(RAW_DATA_KEY, false))
  // The stored task, read while its tab is open, again as it changes.
  const documentQ = useMinionTaskDocument(
    t.id,
    [t.state, t.parts, t.sendCount, t.ackedAt, t.cancelRequestedAt].join(":"),
    shown === "json",
  )
  // The editor's code, ahead of the JSON tab or raw data being opened.
  useEffect(() => preloadJsonEditor(), [])

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
          <TabButton active={shown === "json"} onClick={() => setTab("json")} title="The task as core keeps it: for debugging">
            JSON
          </TabButton>
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
                saveFlag(WRAP_KEY, !wrap)
                setWrap(!wrap)
              }}>
              <WrapTextIcon />
            </Button>
          )}
          {shown === "data" && listing && (
            <Button variant={rawData ? "secondary" : "ghost"} size="icon-xs" title={rawData ? "Raw JSON: show it rendered" : "Rendered: show the raw JSON"}
              aria-label="Show the data as raw JSON" aria-pressed={rawData}
              onClick={() => {
                saveFlag(RAW_DATA_KEY, !rawData)
                setRawData(!rawData)
              }}>
              <BracesIcon />
            </Button>
          )}
          {shown === "data" ? (
            <>
              <Button variant="ghost" size="icon-xs" title="Copy the data as JSON" aria-label="Copy the data" disabled={!data}
                onClick={() => void copyToClipboard(JSON.stringify(data, null, 2), "data")}>
                <CopyIcon />
              </Button>
              <Button variant="ghost" size="icon-xs" title="Download the data as the minion sent it: CBOR, a sequence of its parts" aria-label="Download the raw data"
                render={<a href={`${API_URL}/minion-tasks/${encodeURIComponent(t.id)}/data`} download />}>
                <DownloadIcon />
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" size="icon-xs" title="Copy the output" aria-label="Copy the output" disabled={!actions.hasOutput}
                onClick={() => void actions.copyOutput(detail ? output : undefined)}>
                <CopyIcon />
              </Button>
              <Button variant="ghost" size="icon-xs" title="Download the output as it came, bytes and all" aria-label="Download the raw output"
                disabled={!actions.hasOutput}
                render={actions.hasOutput ? <a href={`${API_URL}/minion-tasks/${encodeURIComponent(t.id)}/output`} download /> : undefined}>
                <DownloadIcon />
              </Button>
            </>
          )}
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
      ) : shown === "data" ? (
        <div className="flex min-h-0 flex-1 flex-col">
          {dataNotes.length > 0 && (
            <p className="border-b px-3 py-1 text-xs text-amber-700 dark:text-amber-400">{dataNotes.join(" ")}</p>
          )}
          {listing && !rawData ? (
            <DirListingView listing={listing} onOpen={onOpenPath} />
          ) : (
            data && (
              <LazyJsonEditor
                value={JSON.stringify(data, null, 2)}
                onChange={noChange}
                readOnly
                fileName={`${t.command}-${t.id.slice(0, 8)}.data.json`}
                aria-label="The task's data as JSON"
                className="min-h-0 flex-1"
              />
            )
          )}
        </div>
      ) : (
        documentQ.data?.minionTask && (
          <LazyJsonEditor
            value={documentQ.data.minionTask.document}
            onChange={noChange}
            readOnly
            fileName={`${t.command}-${t.id.slice(0, 8)}.task.json`}
            aria-label="The task as core keeps it"
            className="min-h-0 flex-1"
          />
        )
      )}
    </div>
  )
}

function TabButton({ active, onClick, title, children }: { active: boolean; onClick: () => void; title?: string; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={title}
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

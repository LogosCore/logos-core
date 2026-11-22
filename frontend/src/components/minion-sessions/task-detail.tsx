import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import {
  BracesIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CopyIcon,
  CornerLeftUpIcon,
  DownloadIcon,
  RotateCcwIcon,
  SearchIcon,
  SquareIcon,
  SquareTerminalIcon,
  StarIcon,
  WrapTextIcon,
  XIcon,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import { useMinionTaskDocument, useMinionTaskFeedback, useMinionTaskOutput } from "@/graphql/hooks/minion-tasks"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { useRecordModals } from "@/hooks/use-record-modals"
import { useSteadyTitle } from "@/hooks/use-steady-title"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { lineOf } from "./command-line"
import type { ConsoleCommand } from "./command-bar"
import { readDirListing } from "./dir-listing"
import { readDirWalk } from "./dir-walk"
import { WalkView } from "./walk-view"
import { FeedbackCounts } from "./feedback-parts"
import { feedbackSummary } from "./feedback-values"
import { TaskFeedbackView } from "./task-feedback"
import { readVolumes } from "./volumes"
import { VolumesView } from "./volumes-view"
import { ProcessListView } from "./process-list-view"
import type { KillTarget } from "./kill-dialog"
import { DirListingView } from "./dir-listing-view"
import { FileView } from "./file-view"
import { UploadView } from "./upload-view"
import { StreamView } from "./stream-view"
import { TunnelView } from "./tunnel-view"
import { RecordingsView } from "./recordings-view"
import { DelegateView } from "./delegate-view"
import { FRONT_FORWARD, FRONT_TERMINAL, FRONT_VPN } from "./tunnel-values"
import type { DeleteTarget } from "./delete-dialog"
import type { UploadTarget } from "./upload-target"
import { TabButton } from "./tab-button"
import type { MenuCommand } from "./action-menu"
import { findMatches, MAX_MATCHES, segments } from "./output-find"
import { FINISHED, markedBy, useTaskActions, type Task } from "./task-actions"
import { desktopName, isDesktopFront } from "./desktop-values"
import { STAR, StateLabel, TaskTimes } from "./task-state"
import { useTaskWaiting } from "@/components/targets/use-task-waiting"
import { useSessionConnectionChoices } from "./use-connection-choices"
import { TaskAs } from "./task-as"
import { CompositeView } from "./composite-view"
import { subtaskText } from "./composite-values"
import { FACTORY } from "./factory-values"
import { ResultFilesView } from "./result-files-view"

// The right of a session's console: the task picked in the list, whole
// (its command is the list's row): where it stands and who gave it, what can be done with it,
// and what came back: the output as text, read again as more comes (Ctrl+F
// finds in it), and any data the command gave, in its result's
// view (a dir_listing as a file browser, a process_list as a table read
// from core) or as raw JSON. Output and data
// download as the minion sent them. What the minion said of it as feedback
// is a tab of its own, its counts in the tab's name. A composite command's
// task has a tab of what its factory's code decided (its status, its own op,
// its subtasks), its status line beside its state; a subtask goes to its
// composite task, shown with its subtasks.
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

/** Who core's own tasks are issued by: no user (a delegate task core queued). */
const NO_ISSUER = "00000000-0000-0000-0000-000000000000"

export function TaskDetail({
  task: t,
  command,
  canWrite,
  canMark,
  onQueued,
  onOpenPath,
  listCommands,
  onDownloadFile,
  downloadCommands,
  onUploadHere,
  uploadCommands,
  onStreamFile,
  streamCommands,
  onDeletePath,
  onWalkPath,
  walkCommands,
  onOpenInFileBrowser,
  deleteCommands,
  onKillProcess,
  killCommands,
  onRefreshProcesses,
  refreshCommands,
  findSignal,
  onShowComposite,
  showingComposite,
}: {
  task: Task
  command?: ConsoleCommand
  canWrite: boolean
  /** Whether the caller marks tasks important: an operator, whatever the session's state. */
  canMark: boolean
  onQueued: (t: Task) => void
  /** Lists another path (a folder opened in a listing): with commandId, or
   *  the task's own command again; absent where the viewer may not give
   *  tasks. */
  onOpenPath?: (path: string, commandId?: string) => void
  /** Downloads a file in a listing, with a download_file command; absent
   *  where the viewer may not give tasks. */
  onDownloadFile?: (path: string, commandId: string) => void
  /** The session's download_file commands: what Download offers. */
  downloadCommands?: readonly MenuCommand[]
  /** Uploads a file to a folder in a listing, with an upload_file command:
   *  the folder, and the names in it when the listing shows them; absent
   *  where the viewer may not give tasks. */
  onUploadHere?: (target: UploadTarget, commandId: string) => void
  /** The session's upload_file commands: what Upload here offers. */
  uploadCommands?: readonly MenuCommand[]
  /** Streams a file in a listing, with a stream_file command; absent where
   *  the viewer may not give tasks. */
  onStreamFile?: (path: string, commandId: string) => void
  /** The session's stream_file commands: what Stream offers. */
  streamCommands?: readonly MenuCommand[]
  /** Deletes an entry of a listing, with a delete_path command (after a
   *  confirmation); absent where the viewer may not give tasks. */
  onDeletePath?: (target: DeleteTarget, commandId: string) => void
  /** Opens a listing's folder in the machine's file browser. */
  onOpenInFileBrowser?: (path: string) => void
  /** The session's delete_path commands: what Delete offers. */
  deleteCommands?: readonly MenuCommand[]
  /** Walks a listing's folder and those under it, with a walk_dir command;
   *  absent where the viewer may not give tasks. */
  onWalkPath?: (path: string, commandId: string) => void
  /** The session's walk_dir commands: what Walk from here offers. */
  walkCommands?: readonly MenuCommand[]
  /** The session's list_dir commands, the task's own first: what List
   *  directory offers. */
  listCommands?: readonly MenuCommand[]
  /** Kills a process of a process list, with a kill_process command (after
   *  a confirmation); absent where the viewer may not give tasks. */
  onKillProcess?: (target: KillTarget, commandId: string) => void
  /** The session's kill_process commands: what Kill offers. */
  killCommands?: readonly MenuCommand[]
  /** Lists the processes again, with a list_processes command: a process
   *  list's Refresh; absent where the viewer may not give tasks. */
  onRefreshProcesses?: (commandId: string) => void
  /** The session's list_processes commands, the task's own first. */
  refreshCommands?: readonly MenuCommand[]
  /** Goes up when Ctrl+F is pressed in the console: the find bar opens. */
  findSignal: number
  /** Shows a composite task with its subtasks in the list, pick picked there. */
  onShowComposite?: (id: string, pick?: string) => void
  /** The composite task the list shows with its subtasks now; "" for none. */
  showingComposite?: string
}) {
  const connections = useSessionConnectionChoices(t.sessionId)
  const held = useTaskWaiting(t.sessionId, { live: true })
  const line = lineOf(t.command, t.args as Record<string, unknown> | null, command, connections)
  const actions = useTaskActions(t, line, { canWrite, canMark, onQueued })
  // Read again when they change: more output, a part with data, its end.
  // Not on every part: a walk sends parts with listings alone, many a
  // second, and its progress (its data) every 2 seconds.
  const outputQ = useMinionTaskOutput(t.id, `${t.outputSize}:${t.dataParts}:${t.state}`, FINISHED.has(t.state))
  const detail = outputQ.data?.minionTask
  const output = detail?.output ?? ""
  const data = (detail?.data as Record<string, unknown> | null | undefined) ?? null
  // A factory command's task's files, those its result named: read with it.
  const files = detail?.files ?? []
  // Its feedback, read when it has some, again as more comes or the
  // session's is marked read (unread ones are marked).
  // Information too, which marks the task in no list.
  const counts = { errors: t.feedbackErrors, warnings: t.feedbackWarnings, infos: t.feedbackInfos }
  const hasFeedback = counts.errors + counts.warnings + counts.infos > 0
  const session = useMinionSession(t.sessionId).data?.minionSession
  const feedbackQ = useMinionTaskFeedback(t.id, `${counts.errors}:${counts.warnings}:${counts.infos}:${session?.feedbackReadAt ?? ""}`, hasFeedback)
  const feedback = feedbackQ.data?.minionTask?.feedback ?? []
  const feedbackTitle = `What the minion said of it: ${feedbackSummary(counts.errors, counts.warnings, counts.infos)}`
  const { open: openRecord } = useRecordModals()
  const sessionId = t.sessionId
  // Whether it gave data, known from its size until it is read (and
  // guessed for a task kept before that: parts with no output are data),
  // so an ls opens on its Data tab at once rather than on an empty Output.
  // A file_download task's file shows from its first chunk, before its data;
  // a screenshot task's picture likewise.
  const isFile = t.result === "file_download" || t.result === "screenshot"
  // A screenshot opens on its picture: what it prints only says which.
  const isShot = t.result === "screenshot"
  // A file_upload task's upload shows from its queueing, before its data.
  const isUpload = t.result === "file_upload" && t.upload != null
  // A file_stream task's stream shows from the minion's announcement; its
  // file, once core kept a copy of all of it, as a download's.
  const isStream = t.result === "file_stream" && t.stream != null
  const keptStream = t.result === "file_stream" && t.file?.state === "COMPLETE"
  // A tunnel task's proxy shows from its queueing: core took its port then.
  const isTunnel = t.tunnel != null
  // A desktop task's: each desktop opened here, recorded.
  const isDesktop = t.tunnel != null && !t.tunnel.reverse && isDesktopFront(t.tunnel.front)
  // Its sessions are recorded: a desktop's, or a terminal's.
  const recorded = isDesktop || (t.tunnel != null && !t.tunnel.reverse && t.tunnel.front === FRONT_TERMINAL)
  // A delegate task's: what it serves and carries.
  const isDelegate = t.delegate != null
  // A composite command's: what its factory's code decided.
  const isComposite = t.composite != null
  const hasData =
    (isFile && t.file != null) ||
    isUpload ||
    isStream ||
    isTunnel ||
    isDelegate ||
    (detail ? data !== null : t.dataSize > 0 || (t.outputSize === 0 && t.parts > 0))
  // The result's view, for data that fits it; the rest shows raw.
  const listing = t.result === "dir_listing" && !t.dataProblem ? readDirListing(data) : null
  const walk = t.result === "dir_walk" && !t.dataProblem ? readDirWalk(data) : null
  const volumes = t.result === "volumes" && !t.dataProblem ? readVolumes(data) : null
  // A process list is read from core a page at a time, not from data.
  const processList = t.result === "process_list" && !t.dataProblem
  const dataNotes = [
    t.dataProblem ? `The data does not fit ${t.result}: ${t.dataProblem}. It shows raw.` : null,
    t.result &&
    !["dir_listing", "dir_walk", "volumes", "process_list", "file_upload", "file_stream"].includes(t.result) &&
    !isFile
      ? `Its result, ${t.result}, is not one this version shows: the data shows raw.`
      : null,
    t.missingParts > 0
      ? `${t.missingParts} ${t.missingParts === 1 ? "part" : "parts"} before the last never came: the data may lack some.`
      : null,
    t.dataTruncated ? "Cut at 4 MiB with the output: the rest was dropped." : null,
  ].filter((n): n is string => n !== null)

  const [tab, setTab] = useState<"output" | "data" | "files" | "feedback" | "recordings" | "composite" | "json" | null>(null)
  // Data alone shows as data; with output, the output first. An upload
  // shows its progress first, whatever it prints, and a screenshot its
  // picture. Feedback and a desktop's or a terminal's recordings only when
  // asked.
  // A composite task opens on what its factory's code decided, until it has
  // output or data to show.
  const compositeShown = isComposite && (tab === "composite" || (tab === null && t.outputSize === 0 && !hasData))
  // A factory command's task opens on its result's files when it printed
  // nothing and gave no data.
  const filesShown = files.length > 0 && (tab === "files" || (tab === null && t.outputSize === 0 && !hasData))
  const shown =
    tab === "json"
      ? "json"
      : compositeShown
        ? "composite"
      : filesShown
        ? "files"
      : tab === "recordings" && recorded
        ? "recordings"
      : tab === "feedback" && hasFeedback
        ? "feedback"
        : tab === "data" && hasData
        ? "data"
        : tab === "output" || !hasData || (t.outputSize > 0 && !isUpload && !isStream && !isTunnel && !isDelegate && !isShot)
          ? "output"
          : "data"
  const [wrap, setWrap] = useState(() => readFlag(WRAP_KEY, true))
  const [rawData, setRawData] = useState(() => readFlag(RAW_DATA_KEY, false))
  // The stored task, read while its tab is open, again as it changes.
  const documentQ = useMinionTaskDocument(
    t.id,
    [t.state, t.parts, t.sendCount, t.ackedAt, t.cancelRequestedAt, t.factory?.instance, t.factory?.acceptedAt, t.factory?.waiting].join(":"),
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
              {isDelegate ? "Delegate" : isFile ? "File" : isUpload ? "Upload" : isStream ? "Stream" : isTunnel ? (t.tunnel?.front === FRONT_FORWARD ? "Forward" : t.tunnel?.front === FRONT_TERMINAL ? "Terminal" : t.tunnel?.front === FRONT_VPN ? "VPN" : t.tunnel && isDesktopFront(t.tunnel.front) ? desktopName(t.tunnel.front) : "Proxy") : "Data"}
            </TabButton>
          )}
          {files.length > 0 && (
            <TabButton active={shown === "files"} onClick={() => setTab("files")} title="The files its factory's result named: kept with the task">
              <span className="inline-flex items-center gap-1.5">
                Files
                <span className="text-muted-foreground tabular-nums">{files.length}</span>
              </span>
            </TabButton>
          )}
          {recorded && (
            <TabButton
              active={shown === "recordings"}
              onClick={() => setTab("recordings")}
              title={isDesktop ? "Each desktop opened here, recorded: they play here" : "Each terminal session, recorded: they play here"}
            >
              <span className="inline-flex items-center gap-1.5">
                Recordings
                {t.recordings > 0 && <span className="text-muted-foreground tabular-nums">{t.recordings}</span>}
              </span>
            </TabButton>
          )}
          {isComposite && (
            <TabButton
              active={shown === "composite"}
              onClick={() => setTab("composite")}
              title="What its factory's code decided: its status, its own op, its subtasks"
            >
              Composite
            </TabButton>
          )}
          {hasFeedback && (
            <TabButton active={shown === "feedback"} onClick={() => setTab("feedback")} title={feedbackTitle}>
              <span className="inline-flex items-center gap-1.5">
                Feedback
                <FeedbackCounts errors={counts.errors} warnings={counts.warnings} infos={counts.infos} className="gap-1.5" title={feedbackTitle} />
              </span>
            </TabButton>
          )}
          <TabButton active={shown === "json"} onClick={() => setTab("json")} title="The task as core keeps it: for debugging">
            JSON
          </TabButton>
        </div>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-0.5 border-l pl-2">
          <StateLabel t={t} held={held} />
          {t.composite?.status && <StatusLine status={t.composite.status} />}
          {t.parentId && (
            <button
              type="button"
              disabled={!onShowComposite}
              onClick={() => onShowComposite?.(t.parentId ?? "", t.parentId ?? undefined)}
              title="A subtask of a composite task: show it, with its subtasks"
              className="inline-flex max-w-full min-w-0 cursor-pointer items-baseline gap-1 text-xs text-violet-700 hover:underline disabled:cursor-default disabled:no-underline dark:text-violet-400"
            >
              <CornerLeftUpIcon className="size-3 shrink-0 self-center" aria-hidden />
              <span className="truncate">subtask{subtaskText(t) ? `: ${subtaskText(t)}` : ""}</span>
            </button>
          )}
          <span className="text-xs text-muted-foreground" title={`Queued ${formatAbsolute(t.queuedAt)}`}>
            by {t.issuedBy === NO_ISSUER ? "core" : (t.issuedByName ?? "someone gone")}
          </span>
          <TaskAs as={t.as} />
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
          {shown === "data" &&
            (listing || walk || volumes || (processList && data) || (isFile && t.file && data) || (isUpload && data) || (isStream && data)) && (
            <Button variant={rawData ? "secondary" : "ghost"} size="icon-xs" title={rawData ? "Raw JSON: show it rendered" : "Rendered: show the raw JSON"}
              aria-label="Show the data as raw JSON" aria-pressed={rawData}
              onClick={() => {
                saveFlag(RAW_DATA_KEY, !rawData)
                setRawData(!rawData)
              }}>
              <BracesIcon />
            </Button>
          )}
          {shown === "recordings" || shown === "composite" || shown === "files" || (shown === "data" && (isTunnel || isDelegate)) ? null : shown === "data" ? (
            <>
              <Button variant="ghost" size="icon-xs" title="Copy the data as JSON" aria-label="Copy the data" disabled={!data}
                onClick={() => void copyToClipboard(JSON.stringify(data, null, 2), "data")}>
                <CopyIcon />
              </Button>
              <Button variant="ghost" size="icon-xs" title="Download the data as the minion sent it: CBOR, a sequence of its parts" aria-label="Download the raw data"
                nativeButton={false}
                render={<a href={`${API_URL}/minion-tasks/${encodeURIComponent(t.id)}/data`} download />}>
                <DownloadIcon />
              </Button>
            </>
          ) : shown === "feedback" ? null : (
            <>
              <Button variant="ghost" size="icon-xs" title="Copy the output" aria-label="Copy the output" disabled={!actions.hasOutput}
                onClick={() => void actions.copyOutput(detail ? output : undefined)}>
                <CopyIcon />
              </Button>
              <Button variant="ghost" size="icon-xs" title="Download the output as it came, bytes and all" aria-label="Download the raw output"
                disabled={!actions.hasOutput}
                nativeButton={!actions.hasOutput}
                render={actions.hasOutput ? <a href={`${API_URL}/minion-tasks/${encodeURIComponent(t.id)}/output`} download /> : undefined}>
                <DownloadIcon />
              </Button>
            </>
          )}
          {actions.canMark ? (
            <Button variant="ghost" size="icon-xs" aria-pressed={!!t.importantAt} disabled={actions.marking} onClick={actions.toggleImportant}
              title={t.importantAt ? `Marked important by ${markedBy(t)}: unmark it (S in the list)` : "Mark it important (S in the list)"}
              aria-label={t.importantAt ? "Unmark the task as important" : "Mark the task important"}>
              <StarIcon className={cn(t.importantAt && STAR)} />
            </Button>
          ) : (
            t.importantAt && (
              <span className="inline-flex size-6 items-center justify-center" title={`Marked important by ${markedBy(t)}`}>
                <StarIcon className={cn("size-3.5", STAR)} aria-label="important" />
              </span>
            )
          )}
          {actions.canRunAgain && (
            <Button variant="ghost" size="icon-xs" title="Run it again" aria-label="Run the task again" disabled={actions.runningAgain} onClick={actions.runAgain}>
              <RotateCcwIcon />
            </Button>
          )}
          {actions.canStop && (
            <Button variant="ghost" size="icon-xs" title={actions.stopTitle} aria-label={actions.stopLabel === "Stop" ? "Stop the task" : "Stop the task now"}
              disabled={actions.stopping} onClick={actions.stop}>
              <SquareIcon />
            </Button>
          )}
        </div>
      </div>
      {t.err && <div className="border-b px-3 py-1 font-mono text-xs whitespace-pre-wrap text-red-600 dark:text-red-400">{t.err}</div>}
      {t.cancelReason && <div className="border-b px-3 py-1 text-xs text-muted-foreground">{t.cancelReason}</div>}

      {shown === "composite" ? (
        <CompositeView task={t} command={command} showingSubtasks={showingComposite === t.id} onShowComposite={onShowComposite} />
      ) : shown === "files" ? (
        <ResultFilesView key={t.id} files={files} />
      ) : shown === "recordings" ? (
        <RecordingsView key={t.id} task={t} canDelete={canMark} />
      ) : shown === "feedback" ? (
        <TaskFeedbackView
          key={t.id}
          entries={feedback}
          errors={counts.errors}
          warnings={counts.warnings}
          loading={feedbackQ.isLoading}
          onOpenSession={() => openRecord({ kind: "session", id: sessionId, tab: "feedback" })}
        />
      ) : shown === "output" ? (
        <OutputView
          output={output}
          loading={outputQ.isLoading}
          running={!FINISHED.has(t.state)}
          empty={
            t.state === "QUEUED" || t.state === "SENT"
              ? "No output yet."
              : FINISHED.has(t.state)
                ? "No output."
                : t.state === "COMPOSING"
                  ? "No output yet: its factory's code runs it, in core (the Composite tab)."
                  : t.state === FACTORY && !t.factory?.acceptedAt
                    ? "No output yet: no instance of its factory has taken it."
                    : "Waiting for output…"
          }
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
          {isDelegate && t.delegate ? (
            <DelegateView delegate={t.delegate} args={t.args} command={command} />
          ) : isTunnel && t.tunnel ? (
            <TunnelView key={t.id} task={t} tunnel={t.tunnel} />
          ) : (isFile || keptStream) && t.file && !rawData ? (
            <FileView key={t.file.id} file={t.file} />
          ) : isUpload && !(rawData && data) ? (
            <UploadView task={t} data={data} />
          ) : isStream && t.stream && !(rawData && data) ? (
            <StreamView task={t} stream={t.stream} />
          ) : walk && !rawData ? (
            <WalkView task={t} walk={walk} onOpenInFileBrowser={onOpenInFileBrowser} />
          ) : volumes && !rawData ? (
            <VolumesView volumes={volumes} onOpenInFileBrowser={onOpenInFileBrowser} />
          ) : processList && !rawData ? (
            <ProcessListView
              task={t}
              hostArch={session?.arch}
              onKill={onKillProcess}
              killCommands={killCommands}
              onOpenPath={onOpenPath}
              listCommands={listCommands}
              onDownload={onDownloadFile}
              downloadCommands={downloadCommands}
              onRefresh={onRefreshProcesses}
              refreshCommands={refreshCommands}
            />
          ) : listing && !rawData ? (
            <DirListingView
              listing={listing}
              onOpen={onOpenPath}
              listCommands={listCommands}
              onDownload={onDownloadFile}
              downloadCommands={downloadCommands}
              onUploadHere={onUploadHere}
              uploadCommands={uploadCommands}
              onStream={onStreamFile}
              streamCommands={streamCommands}
              onDelete={onDeletePath}
              deleteCommands={deleteCommands}
              onWalk={onWalkPath}
              walkCommands={walkCommands}
              onOpenInFileBrowser={onOpenInFileBrowser}
            />
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

/** A composite task's status line beside its state, cut to its room: whole on hover, held while hovered (its factory's code changes it as it goes). */
function StatusLine({ status }: { status: string }) {
  const hover = useSteadyTitle(status)
  return (
    <span className="max-w-full min-w-0 truncate text-xs" {...hover}>
      {status}
    </span>
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

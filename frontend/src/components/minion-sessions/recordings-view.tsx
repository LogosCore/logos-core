import { Suspense, lazy, useEffect, useState } from "react"
import { DownloadIcon, FileTextIcon, FilmIcon, Loader2Icon, PlayIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useDeleteRecording, useMinionDesktopSettings, useRecordings } from "@/graphql/hooks/minion-tasks"
import { useNow } from "@/hooks/use-now"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { downloadTranscript } from "./cast-file"
import { desktopName } from "./desktop-values"
import { LazyRecordingPlayer } from "./lazy-recording-player"
import { LazyTerminalRecordingPlayer } from "./lazy-terminal-recording-player"
import { preloadRecordingPlayer } from "./recording-code"
import {
  RECORDING,
  downloadable,
  fileSize,
  formatClock,
  isTerminalRecording,
  isVideo,
  openedBy,
  playable,
  recordingFileName,
  recordingState,
  settingsSummary,
  videoNote,
  type Recording,
} from "./recording-values"

// A desktop recording made a video: the browser's own player, a chunk of
// its own (its keys are read with Guacamole's interpreter).
const VideoRecordingPlayer = lazy(() => import("./video-recording-player"))
import { preloadTerminalRecordingPlayer } from "./terminal-recording-code"
import { FRONT_TERMINAL, type Tone } from "./tunnel-values"

// A desktop or terminal task's recordings (docs:
// contracts/minion-messages/tunnels.md, Recordings): each desktop opened in
// the web app, or each terminal session (the Terminal tab's, or a raw port
// client's), recorded by core as it went, the latest first, read a page at
// a time from core, and again as one starts, ends or is deleted (the task's
// live updates). One plays above the list; its file downloads (Guacamole's
// format, or an asciicast), and a terminal's output as text; an operator
// deletes one that ended. They stay after the task ends, until the
// operation is deleted.

const API_URL = import.meta.env.VITE_API_URL as string

const TONE: Record<Tone, string> = {
  pending: "text-amber-600 dark:text-amber-400",
  good: "text-emerald-600 dark:text-emerald-400",
  muted: "text-muted-foreground",
  bad: "text-red-600 dark:text-red-400",
}

const GRID_COLS =
  "grid-cols-[3rem_minmax(0,1fr)_minmax(0,1.3fr)_4.5rem_5rem_minmax(0,1.5fr)_7rem]"

type Task = MinionTaskFieldsFragment

export function RecordingsView({ task: t, canDelete }: { task: Task; canDelete: boolean }) {
  const q = useRecordings(t.id, String(t.rev))
  const recordings = q.data?.pages.flatMap((p) => p.recordings.recordings) ?? []
  const front = t.tunnel?.front ?? ""
  const terminal = front === FRONT_TERMINAL
  const fields = useMinionDesktopSettings(front).data?.minionDesktopSettings ?? []
  const [playing, setPlaying] = useState<Recording | null>(null)
  const [deleting, setDeleting] = useState<Recording | null>(null)
  useEffect(() => (terminal ? preloadTerminalRecordingPlayer() : preloadRecordingPlayer()), [terminal])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
      {playing && (
        <div className="flex h-[36rem] max-h-[80vh] shrink-0 flex-col">
          {isTerminalRecording(playing) ? (
            <LazyTerminalRecordingPlayer key={playing.id} recording={playing} onClose={() => setPlaying(null)} />
          ) : isVideo(playing) ? (
            <Suspense
              fallback={
                <div className="flex flex-1 items-center justify-center rounded-md border text-sm text-muted-foreground">Loading the player…</div>
              }
            >
              <VideoRecordingPlayer
                key={playing.id}
                recording={playing}
                settings={settingsSummary(fields, playing.settings)}
                onClose={() => setPlaying(null)}
              />
            </Suspense>
          ) : (
            <LazyRecordingPlayer
              key={playing.id}
              recording={playing}
              settings={settingsSummary(fields, playing.settings)}
              onClose={() => setPlaying(null)}
            />
          )}
        </div>
      )}
      <p className="shrink-0 text-xs text-muted-foreground">
        {terminal ? (
          <>
            Each terminal session is recorded, here or on the raw port: what the process wrote, what was typed (a password typed at a
            prompt too), and the terminal&apos;s sizes. They stay until the operation is deleted.
          </>
        ) : (
          <>
            Each {desktopName(front)} desktop opened here is recorded: what it showed, the mouse, and the keys typed in it (none for a
            view-only one). A client of your own on the port is not. They stay until the operation is deleted.
          </>
        )}
      </p>
      <div className="flex min-h-72 flex-1 flex-col">
        {q.error ? (
          <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{q.error.message}</div>
        ) : (
          <VirtualizedDataTable
            items={recordings}
            isLoading={q.isLoading}
            isFetchingNextPage={q.isFetchingNextPage}
            hasNextPage={q.hasNextPage}
            fetchNextPage={() => void q.fetchNextPage()}
            gridCols={GRID_COLS}
            entityNoun="recordings"
            header={
              <>
                <div>#</div>
                <div>Opened by</div>
                <div>When</div>
                <div className="text-right">Length</div>
                <div className="text-right">Size</div>
                <div>State</div>
                <div />
              </>
            }
            emptyState={
              <>
                <FilmIcon className="size-8 opacity-50" />
                <p className="text-sm">No recording yet.</p>
                <p className="max-w-md text-center text-xs">
                  {terminal
                    ? "Open a terminal (on the Terminal tab, or with a client of the raw port): each session is recorded as it goes, and plays here once it ended."
                    : `Open the desktop (Open desktop, on the ${desktopName(front)} tab): each one opened is recorded as it goes, and plays here once it ended.`}
                </p>
              </>
            }
            renderRow={(r) => (
              <RecordingRow
                r={r}
                active={playing?.id === r.id}
                canDelete={canDelete}
                onPlay={() => setPlaying(r)}
                onDelete={() => setDeleting(r)}
              />
            )}
          />
        )}
      </div>
      <DeleteRecordingDialog
        taskId={t.id}
        recording={deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        onDeleted={(r) => {
          if (playing?.id === r.id) setPlaying(null)
        }}
      />
    </div>
  )
}

function RecordingRow({
  r,
  active,
  canDelete,
  onPlay,
  onDelete,
}: {
  r: Recording
  active: boolean
  canDelete: boolean
  onPlay: () => void
  onDelete: () => void
}) {
  const state = recordingState(r)
  const video = videoNote(r)
  const terminal = isTerminalRecording(r)
  const who = openedBy(r)
  // A terminal's: its stream, and the raw port's when no one signed in.
  const detail = terminal ? `stream ${r.stream}${r.username ? "" : " · raw port"}` : r.keys ? null : "view only"
  const [saving, setSaving] = useState(false)
  return (
    <div className={dataTableRowClass(GRID_COLS, cn(active && "bg-muted/60"))}>
      <span className="font-mono text-xs text-muted-foreground tabular-nums">{r.number}</span>
      <span className="flex min-w-0 flex-col text-xs">
        <span className="truncate" title={r.client || who}>
          {who}
        </span>
        {detail && <span className="truncate text-muted-foreground">{detail}</span>}
      </span>
      <span className="flex min-w-0 flex-col text-xs tabular-nums">
        <span className="truncate">{formatDateTime(r.startedAt)}</span>
        {r.endedAt && <span className="truncate text-muted-foreground">to {formatDateTime(r.endedAt)}</span>}
      </span>
      <span className="text-right text-xs tabular-nums">
        {r.state === RECORDING ? <LiveLength since={r.startedAt} /> : r.state === "FAILED" ? "" : formatClock(r.durationMs)}
      </span>
      <span className="text-right text-xs tabular-nums" title={isVideo(r) ? `A video now; ${formatBytes(r.size)} as recorded` : undefined}>
        {r.state === "FAILED" ? "" : formatBytes(fileSize(r))}
      </span>
      <span className="flex min-w-0 flex-col">
        <span className={cn("text-xs", TONE[state.tone])}>{state.label}</span>
        {video && (
          <span className="truncate text-xs text-muted-foreground" title={video.title}>
            {video.text}
          </span>
        )}
        {state.detail && (
          <span className="truncate text-xs text-muted-foreground" title={state.detail}>
            {state.detail}
          </span>
        )}
      </span>
      <span className="flex items-center justify-end gap-0.5">
        <Button
          variant={active ? "secondary" : "ghost"}
          size="icon-xs"
          title={
            playable(r)
              ? `Play recording #${r.number}`
              : r.state === RECORDING
                ? `It plays once its ${terminal ? "terminal" : "desktop"} closes`
                : "Nothing to play"
          }
          aria-label={`Play recording #${r.number}`}
          disabled={!playable(r)}
          onClick={onPlay}
        >
          <PlayIcon />
        </Button>
        {downloadable(r) && (
          <Button
            variant="ghost"
            size="icon-xs"
            title={
              terminal
                ? `Download recording #${r.number}: an asciicast, which asciinema plays (asciinema play file.cast)`
                : isVideo(r)
                  ? `Download recording #${r.number}: a video (H.264, MP4)`
                  : `Download recording #${r.number}: Guacamole's format, which guacenc makes a video of`
            }
            aria-label={`Download recording #${r.number}`}
            nativeButton={false}
            render={<a href={`${API_URL}${r.path}`} download={recordingFileName(r, isVideo(r) ? "mp4" : undefined)} />}
          >
            <DownloadIcon />
          </Button>
        )}
        {terminal && downloadable(r) && (
          <Button
            variant="ghost"
            size="icon-xs"
            title={`Download recording #${r.number}'s output as text: the lines the terminal showed, without colours`}
            aria-label={`Download recording #${r.number}'s output as text`}
            disabled={saving}
            onClick={() => {
              setSaving(true)
              downloadTranscript(r)
                .catch((err: unknown) => toast.error(`The transcript could not be made: ${errorMessage(err, "it could not be read")}`))
                .finally(() => setSaving(false))
            }}
          >
            {saving ? <Loader2Icon className="animate-spin" /> : <FileTextIcon />}
          </Button>
        )}
        {canDelete && r.state !== RECORDING && (
          <Button variant="ghost" size="icon-xs" title={`Delete recording #${r.number}`} aria-label={`Delete recording #${r.number}`} onClick={onDelete}>
            <Trash2Icon />
          </Button>
        )}
      </span>
    </div>
  )
}

/** How long a recording has gone, live. */
function LiveLength({ since }: { since: string }) {
  const now = useNow(1000)
  return <>{formatClock(now - Date.parse(since))}</>
}

function DeleteRecordingDialog({
  taskId,
  recording,
  onOpenChange,
  onDeleted,
}: {
  taskId: string
  recording: Recording | null
  onOpenChange: (open: boolean) => void
  onDeleted: (r: Recording) => void
}) {
  const del = useDeleteRecording(taskId)
  const [failure, setFailure] = useState<string | null>(null)
  const r = recording
  return (
    <Dialog
      open={r !== null}
      onOpenChange={(open) => {
        if (!open) setFailure(null)
        onOpenChange(open)
      }}
    >
      <DialogContent className="sm:max-w-md">
        {r && (
          <form
            className="flex min-w-0 flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault()
              setFailure(null)
              del.mutate(r.id, {
                onSuccess: () => {
                  toast.success(`Recording #${r.number} deleted`)
                  onDeleted(r)
                  onOpenChange(false)
                },
                onError: (err) => setFailure(errorMessage(err, "Could not delete it")),
              })
            }}
          >
            <DialogHeader>
              <DialogTitle>Delete recording #{r.number}?</DialogTitle>
              <DialogDescription>
                {isTerminalRecording(r)
                  ? `The terminal session of ${openedBy(r)} at ${formatDateTime(r.startedAt)}: its file goes with it, what it showed and what was typed in it. This cannot be undone.`
                  : `The ${desktopName(r.front)} desktop ${r.username} opened at ${formatDateTime(r.startedAt)}: its file goes with it, what it showed and the keys typed in it. This cannot be undone.`}
              </DialogDescription>
            </DialogHeader>
            {failure && <p className="text-sm text-destructive">{failure}</p>}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" disabled={del.isPending}>
                {del.isPending ? <Loader2Icon className="animate-spin" /> : <Trash2Icon />}
                Delete
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

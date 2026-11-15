import { useEffect, useState } from "react"
import { DownloadIcon, FilmIcon, Loader2Icon, PlayIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useDeleteDesktopRecording, useDesktopRecordings, useMinionDesktopSettings } from "@/graphql/hooks/minion-tasks"
import { useNow } from "@/hooks/use-now"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { desktopName } from "./desktop-values"
import { LazyRecordingPlayer } from "./lazy-recording-player"
import { preloadRecordingPlayer } from "./recording-code"
import {
  RECORDING,
  downloadable,
  formatClock,
  playable,
  recordingFileName,
  recordingState,
  settingsSummary,
  type Recording,
} from "./recording-values"
import type { Tone } from "./tunnel-values"

// A desktop task's recordings (docs: contracts/minion-messages/tunnels.md,
// Recordings): each desktop opened in the web app, recorded by core as it
// went, the latest first, read a page at a time from core, and again as one
// starts, ends or is deleted (the task's live updates). One plays above the
// list, where its desktop showed; its file downloads (Guacamole's format);
// an operator deletes one that ended. They stay after the task ends, until
// the operation is deleted.

const API_URL = import.meta.env.VITE_API_URL as string

const TONE: Record<Tone, string> = {
  pending: "text-amber-600 dark:text-amber-400",
  good: "text-emerald-600 dark:text-emerald-400",
  muted: "text-muted-foreground",
  bad: "text-red-600 dark:text-red-400",
}

const GRID_COLS =
  "grid-cols-[3rem_minmax(0,1fr)_minmax(0,1.3fr)_4.5rem_5rem_minmax(0,1.5fr)_5.5rem]"

type Task = MinionTaskFieldsFragment

export function RecordingsView({ task: t, canDelete }: { task: Task; canDelete: boolean }) {
  const q = useDesktopRecordings(t.id, String(t.rev))
  const recordings = q.data?.pages.flatMap((p) => p.desktopRecordings.recordings) ?? []
  const front = t.tunnel?.front ?? ""
  const fields = useMinionDesktopSettings(front).data?.minionDesktopSettings ?? []
  const [playing, setPlaying] = useState<Recording | null>(null)
  const [deleting, setDeleting] = useState<Recording | null>(null)
  useEffect(() => preloadRecordingPlayer(), [])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
      {playing && (
        <div className="flex h-[36rem] max-h-[80vh] shrink-0 flex-col">
          <LazyRecordingPlayer
            key={playing.id}
            recording={playing}
            settings={settingsSummary(fields, playing.settings)}
            onClose={() => setPlaying(null)}
          />
        </div>
      )}
      <p className="shrink-0 text-xs text-muted-foreground">
        Each {desktopName(front)} desktop opened here is recorded: what it showed, the mouse, and the keys typed in it (none for a
        view-only one). A client of your own on the port is not. They stay until the operation is deleted.
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
                  Open the desktop (Open desktop, on the {desktopName(front)} tab): each one opened is recorded as it goes, and plays here
                  once it ended.
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
  return (
    <div className={dataTableRowClass(GRID_COLS, cn(active && "bg-muted/60"))}>
      <span className="font-mono text-xs text-muted-foreground tabular-nums">{r.number}</span>
      <span className="flex min-w-0 flex-col text-xs">
        <span className="truncate" title={r.username}>
          {r.username}
        </span>
        {!r.keys && <span className="truncate text-muted-foreground">view only</span>}
      </span>
      <span className="flex min-w-0 flex-col text-xs tabular-nums">
        <span className="truncate">{formatDateTime(r.startedAt)}</span>
        {r.endedAt && <span className="truncate text-muted-foreground">to {formatDateTime(r.endedAt)}</span>}
      </span>
      <span className="text-right text-xs tabular-nums">
        {r.state === RECORDING ? <LiveLength since={r.startedAt} /> : r.state === "FAILED" ? "" : formatClock(r.durationMs)}
      </span>
      <span className="text-right text-xs tabular-nums">{r.state === "FAILED" ? "" : formatBytes(r.size)}</span>
      <span className="flex min-w-0 flex-col">
        <span className={cn("text-xs", TONE[state.tone])}>{state.label}</span>
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
          title={playable(r) ? `Play recording #${r.number}` : r.state === RECORDING ? "It plays once its desktop closes" : "Nothing to play"}
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
            title={`Download recording #${r.number}: Guacamole's format, which guacenc makes a video of`}
            aria-label={`Download recording #${r.number}`}
            nativeButton={false}
            render={<a href={`${API_URL}${r.path}`} download={recordingFileName(r)} />}
          >
            <DownloadIcon />
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
  const del = useDeleteDesktopRecording(taskId)
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
                The {desktopName(r.front)} desktop {r.username} opened at {formatDateTime(r.startedAt)}: its file goes with it, what it
                showed and the keys typed in it. This cannot be undone.
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

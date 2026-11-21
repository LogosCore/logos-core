import { useEffect, useRef, useState } from "react"
import { useFullscreen } from "@/hooks/use-fullscreen"
import { DownloadIcon, ExpandIcon, KeyboardIcon, XIcon, ShrinkIcon } from "lucide-react"
import Guacamole from "@/vendor/guacamole/guacamole-common"
import { Button } from "@/components/ui/button"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { desktopName } from "./desktop-values"
import { formatClock, lineAt, recordingFileName, typedLines, type Recording, type TypedLine } from "./recording-values"

// A desktop's recording made a video (docs: Tunnels, Recordings): H.264 in
// MP4, played by the browser's own player as core serves it (in ranges, to
// seek in). What was typed, line by line, from its keys: core kept them
// beside the video, read here as Guacamole's player reads a recording's
// (its KeyEventInterpreter): a line plays from where it began.

const API_URL = import.meta.env.VITE_API_URL as string

export interface VideoRecordingPlayerProps {
  recording: Recording
  /** What the desktop was opened with, for the header. */
  settings: string[]
  onClose: () => void
}

/** A recording's keys, as core keeps them: guacd's first sync, and each key instruction's args. */
interface RecordingKeys {
  start: string
  keys: [string, string, string][]
}

export default function VideoRecordingPlayer({ recording: rec, settings, onClose }: VideoRecordingPlayerProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const fullscreen = useFullscreen(rootRef)
  const videoRef = useRef<HTMLVideoElement>(null)
  const keysRef = useRef<HTMLDivElement>(null)
  const [keysOpen, setKeysOpen] = useState(rec.keys)
  const [lines, setLines] = useState<TypedLine[] | null>(null)
  const [keysError, setKeysError] = useState<string | null>(null)
  const [position, setPosition] = useState(0)
  const url = `${API_URL}${rec.path}`

  // Its keys, as Guacamole's player would read them from the recording.
  useEffect(() => {
    if (!rec.keys) return
    const ac = new AbortController()
    fetch(`${url}/keys`, { credentials: "same-origin", signal: ac.signal })
      .then(async (res) => {
        if (!res.ok) throw new Error(`core answered ${res.status}`)
        const k = (await res.json()) as RecordingKeys
        const interpreter = new Guacamole.KeyEventInterpreter(Number(k.start) || 0)
        for (const args of k.keys) interpreter.handleKeyEvent(args)
        setLines(typedLines(interpreter.getEvents()))
      })
      .catch((err: unknown) => {
        if (!ac.signal.aborted) setKeysError(err instanceof Error ? err.message : "they could not be read")
      })
    return () => ac.abort()
  }, [url, rec.keys])

  const current = lines ? lineAt(lines, position) : -1
  useEffect(() => {
    if (current < 0) return
    keysRef.current?.querySelector(`[data-line="${current}"]`)?.scrollIntoView({ block: "nearest" })
  }, [current])

  const seekTo = (ms: number) => {
    const v = videoRef.current
    if (!v) return
    v.currentTime = ms / 1000
    void v.play()
  }

  return (
    <div ref={rootRef} className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border bg-background">
      <div className="flex min-h-8 flex-wrap items-center gap-1 border-b px-2 py-1 text-xs">
        <span className="flex min-w-0 flex-1 items-baseline gap-1.5">
          <span className="font-medium">
            Recording #{rec.number} · {desktopName(rec.front)}
          </span>
          <span className="truncate text-muted-foreground">
            {rec.username} · {formatDateTime(rec.startedAt)}
            {settings.length > 0 && ` · ${settings.join(" · ")}`}
          </span>
        </span>
        <Button
          variant={keysOpen ? "secondary" : "ghost"}
          size="xs"
          aria-pressed={keysOpen}
          title="What was typed in it, line by line: a line plays from where it began"
          onClick={() => setKeysOpen((o) => !o)}
        >
          <KeyboardIcon />
          Keys
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title="Download the video (H.264, MP4)"
          aria-label="Download the video"
          nativeButton={false}
          render={<a href={url} download={recordingFileName(rec, "mp4")} />}
        >
          <DownloadIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title={fullscreen.full ? "Leave full screen" : "Full screen (Esc leaves it too)"}
          aria-label={fullscreen.full ? "Leave full screen" : "Full screen"}
          aria-pressed={fullscreen.full}
          onClick={fullscreen.toggle}
        >
          {fullscreen.full ? <ShrinkIcon /> : <ExpandIcon />}
        </Button>
        <Button variant="ghost" size="icon-xs" title="Close the recording" aria-label="Close the recording" onClick={onClose}>
          <XIcon />
        </Button>
      </div>
      <div className="flex min-h-0 flex-1">
        <div className="flex min-h-0 min-w-0 flex-1 items-center justify-center bg-black">
          <video
            ref={videoRef}
            src={url}
            controls
            autoPlay
            preload="metadata"
            aria-label={`Recording #${rec.number} of the ${desktopName(rec.front)} desktop`}
            className="max-h-full max-w-full"
            onTimeUpdate={(e) => setPosition(e.currentTarget.currentTime * 1000)}
          />
        </div>
        {keysOpen && (
          <div ref={keysRef} className="flex w-64 shrink-0 flex-col overflow-y-auto border-l text-xs">
            {!rec.keys ? (
              <p className="p-2 text-muted-foreground">No keys: the desktop was view only.</p>
            ) : keysError ? (
              <p className="p-2 text-red-600 dark:text-red-400">The keys could not be read: {keysError}</p>
            ) : lines === null ? (
              <p className="p-2 text-muted-foreground">Reading the keys…</p>
            ) : lines.length === 0 ? (
              <p className="p-2 text-muted-foreground">Nothing was typed.</p>
            ) : (
              lines.map((l, i) => (
                <button
                  key={i}
                  type="button"
                  data-line={i}
                  title={`Play from ${formatClock(l.at)}`}
                  onClick={() => seekTo(l.at)}
                  className={cn(
                    "flex cursor-pointer items-baseline gap-2 border-b px-2 py-1 text-left hover:bg-muted/50",
                    i === current && "bg-muted",
                  )}
                >
                  <span className="shrink-0 text-muted-foreground tabular-nums">{formatClock(l.at)}</span>
                  <span className="min-w-0 font-mono break-all whitespace-pre-wrap">{l.text}</span>
                </button>
              ))
            )}
          </div>
        )}
      </div>
    </div>
  )
}

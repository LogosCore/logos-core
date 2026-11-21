import { useEffect, useRef, useState } from "react"
import { useFullscreen } from "@/hooks/use-fullscreen"
import { DownloadIcon, ExpandIcon, KeyboardIcon, LoaderIcon, PauseIcon, PlayIcon, XIcon, ShrinkIcon } from "lucide-react"
import Guacamole from "@/vendor/guacamole/guacamole-common"
import { Button } from "@/components/ui/button"
import { Slider } from "@/components/ui/slider"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { desktopName, fitScale } from "./desktop-values"
import { recordingTunnel } from "./recording-tunnel"
import { formatClock, lineAt, recordingFileName, typedLines, type Recording, type TypedLine } from "./recording-values"

// A desktop's recording, played (a chunk of its own: recording-code.ts):
// Guacamole's own player, reading the recording's file from core as it
// comes, plays from its first frames while the rest is read, fitted to the
// frame as the desktop was. What was typed, line by line, once it is all
// read: a line plays from where it began.

const API_URL = import.meta.env.VITE_API_URL as string

export interface RecordingPlayerProps {
  recording: Recording
  /** What it opened with, as its front's form says (settingsSummary). */
  settings: readonly string[]
  onClose: () => void
}

export function RecordingPlayer({ recording: rec, settings, onClose }: RecordingPlayerProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const fullscreen = useFullscreen(rootRef)
  const frameRef = useRef<HTMLDivElement>(null)
  const keysRef = useRef<HTMLDivElement>(null)
  const playerRef = useRef<Guacamole.SessionRecording | null>(null)
  // The operator wants it playing: one paused at the end of what was read,
  // with more to come, goes on as it comes.
  const want = useRef(true)
  const waiting = useRef(true)
  const seekingRef = useRef(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [duration, setDuration] = useState(0)
  const [read, setRead] = useState(0)
  const [position, setPosition] = useState(0)
  // While the bar is dragged: where to, before it is let go.
  const [scrub, setScrub] = useState<number | null>(null)
  const [playing, setPlaying] = useState(false)
  const [seeking, setSeeking] = useState(false)
  const [lines, setLines] = useState<TypedLine[] | null>(null)
  const [keysOpen, setKeysOpen] = useState(rec.keys)

  useEffect(() => {
    const frame = frameRef.current
    if (!frame) return
    const player = new Guacamole.SessionRecording(recordingTunnel(`${API_URL}${rec.path}`), 250)
    playerRef.current = player
    want.current = waiting.current = true
    const display = player.getDisplay()
    const element = display.getElement()
    frame.appendChild(element)
    const fit = () => display.scale(fitScale(frame.clientWidth, frame.clientHeight, display.getWidth(), display.getHeight()))
    display.onresize = fit
    let done = false
    const goOn = () => {
      if (waiting.current && want.current && !seekingRef.current) {
        waiting.current = false
        player.play()
      }
    }
    player.onprogress = (d, size) => {
      setDuration(d)
      setRead(size)
      goOn()
    }
    player.onload = () => {
      done = true
      setLoaded(true)
      setDuration(player.getDuration())
      goOn()
    }
    player.onerror = (message) => setError(message || "The recording could not be read")
    player.onplay = () => setPlaying(true)
    player.onpause = () => {
      setPlaying(false)
      if (seekingRef.current) return
      // At the end of what was read: on as more comes; at its end, done.
      if (want.current && !done) waiting.current = true
      else want.current = false
    }
    player.onseek = (p) => setPosition(p)
    player.onkeyevents = (events) => setLines(typedLines(events))
    const observer = new ResizeObserver(fit)
    observer.observe(frame)
    player.connect()
    return () => {
      observer.disconnect()
      player.onprogress = player.onload = player.onerror = player.onplay = player.onpause = player.onseek = null
      player.onkeyevents = null
      player.pause()
      player.abort()
      element.remove()
      playerRef.current = null
    }
  }, [rec.path])

  const seekTo = (ms: number, then?: () => void) => {
    const player = playerRef.current
    if (!player) return
    seekingRef.current = true
    setSeeking(true)
    player.seek(ms, () => {
      seekingRef.current = false
      setSeeking(false)
      setPosition(ms)
      then?.()
    })
  }

  // The whole recording's length, as its record says, until it is read.
  const total = loaded ? duration : Math.max(duration, rec.durationMs)
  const atEnd = loaded && position >= duration - 50
  const toggle = () => {
    const player = playerRef.current
    if (!player) return
    if (playing || player.isPlaying()) {
      want.current = false
      waiting.current = false
      player.pause()
      return
    }
    want.current = true
    if (atEnd) {
      seekTo(0, () => player.play())
      return
    }
    waiting.current = true
    player.play()
    if (player.isPlaying()) waiting.current = false
  }

  const current = lines ? lineAt(lines, scrub ?? position) : -1
  useEffect(() => {
    if (current < 0) return
    keysRef.current?.querySelector(`[data-line="${current}"]`)?.scrollIntoView({ block: "nearest" })
  }, [current])

  const percent = rec.size > 0 ? Math.min(99, Math.floor((read / rec.size) * 100)) : 0
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
          title="Download the recording: Guacamole's format, which guacenc makes a video of"
          aria-label="Download the recording"
          nativeButton={false}
          render={<a href={`${API_URL}${rec.path}`} download={recordingFileName(rec)} />}
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
        <div className="relative flex min-h-0 min-w-0 flex-1">
          <div
            ref={frameRef}
            aria-label={`Recording #${rec.number} of the ${desktopName(rec.front)} desktop`}
            className="flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-black"
          />
          {(error || (!loaded && read === 0) || seeking) && (
            <div
              className={cn(
                "pointer-events-none absolute inset-0 flex items-center justify-center p-4 text-center text-sm",
                error && "pointer-events-auto bg-background/80 text-red-600 dark:text-red-400",
              )}
            >
              {error ?? (
                <span className="flex items-center gap-1.5 rounded-md bg-background/80 px-2 py-1 text-muted-foreground">
                  <LoaderIcon className="size-3.5 animate-spin" />
                  {seeking ? "Seeking…" : "Reading the recording…"}
                </span>
              )}
            </div>
          )}
        </div>
        {keysOpen && (
          <div ref={keysRef} className="flex w-64 shrink-0 flex-col overflow-y-auto border-l text-xs">
            {!rec.keys ? (
              <p className="p-2 text-muted-foreground">No keys: the desktop was view only.</p>
            ) : lines === null ? (
              <p className="p-2 text-muted-foreground">{error ? "The keys are read with the recording." : "Read once the recording is."}</p>
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
      <div className="flex items-center gap-2 border-t px-2 py-1 text-xs">
        <Button
          variant="ghost"
          size="icon-xs"
          title={playing ? "Pause" : atEnd ? "Play it again" : "Play"}
          aria-label={playing ? "Pause" : "Play"}
          disabled={!!error && duration === 0}
          onClick={toggle}
        >
          {playing ? <PauseIcon /> : <PlayIcon />}
        </Button>
        <span className="shrink-0 text-muted-foreground tabular-nums">
          {formatClock(scrub ?? position)} / {formatClock(total)}
        </span>
        <Slider
          className="min-w-24 flex-1"
          aria-label="Position"
          min={0}
          max={Math.max(total, 1)}
          step={100}
          value={Math.min(scrub ?? position, Math.max(total, 1))}
          disabled={duration === 0}
          onValueChange={(v) => setScrub(v)}
          onValueCommitted={(v) => {
            setScrub(null)
            seekTo(v)
          }}
        />
        {!loaded && !error && (
          <span className="shrink-0 text-muted-foreground tabular-nums" title="Read from core so far: it plays as it comes">
            read {percent}%
          </span>
        )}
      </div>
    </div>
  )
}

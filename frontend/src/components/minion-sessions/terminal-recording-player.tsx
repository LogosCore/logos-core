import { useEffect, useMemo, useRef, useState } from "react"
import { useFullscreen } from "@/hooks/use-fullscreen"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { DownloadIcon, ExpandIcon, FileTextIcon, KeyboardIcon, Loader2Icon, LoaderIcon, PauseIcon, PlayIcon, SkipForwardIcon, XIcon, ShrinkIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Slider } from "@/components/ui/slider"
import { formatDateTime } from "@/lib/date-format"
import { cn } from "@/lib/utils"
import { downloadTranscript, fetchCast } from "./cast-file"
import { inputLines, parseSize, playTimes, playedBy, type Cast } from "./cast-values"
import { formatClock, lineAt, openedBy, recordingFileName, type Recording } from "./recording-values"

// A terminal's recording, played (a chunk of its own:
// terminal-recording-code.ts): its asciicast read from core whole, drawn by
// xterm.js as the Terminal tab draws one, at the terminal's own size and
// its sizes as they changed. Pauses past two seconds are skipped unless
// asked; what was typed, line by line: a line plays from where it began.

const API_URL = import.meta.env.VITE_API_URL as string

/** The longest pause played, while pauses are skipped. */
const MAX_PAUSE = 2000
const SPEEDS = [1, 2, 4, 8, 16] as const

export interface TerminalRecordingPlayerProps {
  recording: Recording
  onClose: () => void
}

/**
 * A recording's events written to a terminal up to a position: forward from
 * where it is, or again from its start. Writes, sizes and the reset all go
 * through the terminal's write queue, so each comes after what was written
 * before it.
 */
class Playback {
  played = 0
  private readonly term: Terminal
  private readonly cast: Cast

  constructor(term: Terminal, cast: Cast) {
    this.term = term
    this.cast = cast
  }

  show(times: readonly number[], position: number) {
    const n = playedBy(times, position)
    const term = this.term
    if (n < this.played) {
      const { width, height } = this.cast.header
      term.write("", () => {
        term.reset()
        term.resize(width, height)
      })
      this.played = 0
    }
    let out = ""
    for (let i = this.played; i < n; i++) {
      const e = this.cast.events[i]
      if (e.code === "o") {
        out += e.data
      } else if (e.code === "r") {
        const size = parseSize(e.data)
        if (!size) continue
        term.write(out, () => term.resize(size.cols, size.rows))
        out = ""
      }
    }
    if (out) term.write(out)
    this.played = n
  }
}

export function TerminalRecordingPlayer({ recording: rec, onClose }: TerminalRecordingPlayerProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const fullscreen = useFullscreen(rootRef)
  const frameRef = useRef<HTMLDivElement>(null)
  const keysRef = useRef<HTMLDivElement>(null)
  const playbackRef = useRef<Playback | null>(null)
  // Where it is, for the loop, which reads it as it starts.
  const positionRef = useRef(0)
  const [cast, setCast] = useState<Cast | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [skip, setSkip] = useState(true)
  const [speed, setSpeed] = useState<number>(1)
  const [position, setPosition] = useState(0)
  // While the bar is dragged: where to, before it is let go.
  const [scrub, setScrub] = useState<number | null>(null)
  const [playing, setPlaying] = useState(false)
  const [keysOpen, setKeysOpen] = useState(true)
  const [saving, setSaving] = useState(false)
  // Counts the moves: playing goes on from each.
  const [moves, setMoves] = useState(0)

  useEffect(() => {
    const ac = new AbortController()
    fetchCast(rec.path, ac.signal).then(
      (c) => {
        setCast(c)
        setPlaying(true)
      },
      (err: unknown) => {
        if (!ac.signal.aborted) setError(err instanceof Error ? err.message : String(err))
      },
    )
    return () => ac.abort()
  }, [rec.path])

  const times = useMemo(() => (cast ? playTimes(cast.events, skip ? MAX_PAUSE : null) : []), [cast, skip])
  const total = times.length > 0 ? times[times.length - 1] : 0
  const lines = useMemo(() => (cast ? inputLines(cast.events, times) : null), [cast, times])

  useEffect(() => {
    const frame = frameRef.current
    if (!frame || !cast) return
    const term = new Terminal({
      cols: cast.header.width,
      rows: cast.header.height,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 13,
      scrollback: 5000,
      disableStdin: true,
      cursorBlink: false,
      theme: { background: "#0a0a0a" },
    })
    term.open(frame)
    playbackRef.current = new Playback(term, cast)
    return () => {
      playbackRef.current = null
      term.dispose()
    }
  }, [cast])

  const moveTo = (pos: number) => {
    positionRef.current = pos
    setPosition(pos)
    setMoves((m) => m + 1)
    playbackRef.current?.show(times, pos)
  }

  // Playing: on from where it is, at its speed, to its end.
  useEffect(() => {
    if (!playing) return
    let raf = 0
    const t0 = performance.now()
    const base = positionRef.current >= total ? 0 : positionRef.current
    const tick = () => {
      const pos = Math.min(base + (performance.now() - t0) * speed, total)
      positionRef.current = pos
      setPosition(pos)
      playbackRef.current?.show(times, pos)
      if (pos >= total) {
        setPlaying(false)
        return
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [playing, speed, times, total, moves])

  const toggleSkip = () => {
    if (!cast) return
    const next = !skip
    const nextTimes = playTimes(cast.events, next ? MAX_PAUSE : null)
    // The same moment, where it plays now: its last event's.
    const played = playbackRef.current?.played ?? 0
    const pos = played > 0 ? nextTimes[played - 1] : 0
    positionRef.current = pos
    setPosition(pos)
    setSkip(next)
  }

  const saveText = () => {
    setSaving(true)
    downloadTranscript(rec, cast ?? undefined)
      .catch((err: unknown) => toast.error(`The transcript could not be made: ${err instanceof Error ? err.message : String(err)}`))
      .finally(() => setSaving(false))
  }

  const current = lines ? lineAt(lines, scrub ?? position) : -1
  useEffect(() => {
    if (current < 0) return
    keysRef.current?.querySelector(`[data-line="${current}"]`)?.scrollIntoView({ block: "nearest" })
  }, [current])

  const atEnd = cast !== null && position >= total
  const size = cast ? `${cast.header.width}×${cast.header.height}` : `${rec.width}×${rec.height}`
  return (
    <div ref={rootRef} className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border bg-background">
      <div className="flex min-h-8 flex-wrap items-center gap-1 border-b px-2 py-1 text-xs">
        <span className="flex min-w-0 flex-1 items-baseline gap-1.5">
          <span className="font-medium">Recording #{rec.number} · terminal</span>
          <span className="truncate text-muted-foreground">
            {openedBy(rec)} · {formatDateTime(rec.startedAt)} · {size}
            {rec.stream > 0 && ` · stream ${rec.stream}`}
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
          variant={skip ? "secondary" : "ghost"}
          size="xs"
          aria-pressed={skip}
          title={`Skip pauses: each one longer than ${MAX_PAUSE / 1000} seconds plays as ${MAX_PAUSE / 1000} seconds`}
          disabled={!cast}
          onClick={toggleSkip}
        >
          <SkipForwardIcon />
          Skip pauses
        </Button>
        <Button
          variant="ghost"
          size="xs"
          className="tabular-nums"
          title="Speed: click for the next"
          aria-label={`Speed ${speed}×`}
          onClick={() => setSpeed((s) => SPEEDS[(SPEEDS.indexOf(s as (typeof SPEEDS)[number]) + 1) % SPEEDS.length])}
        >
          {speed}×
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title="Download the recording: an asciicast, which asciinema plays (asciinema play file.cast)"
          aria-label="Download the recording"
          nativeButton={false}
          render={<a href={`${API_URL}${rec.path}`} download={recordingFileName(rec)} />}
        >
          <DownloadIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title="Download its output as text: the lines the terminal showed, without colours"
          aria-label="Download its output as text"
          disabled={saving}
          onClick={saveText}
        >
          {saving ? <Loader2Icon className="animate-spin" /> : <FileTextIcon />}
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
        <div className="relative flex min-h-0 min-w-0 flex-1 overflow-auto bg-[#0a0a0a] p-2">
          <div ref={frameRef} aria-label={`Recording #${rec.number} of the terminal`} className="w-max" />
          {(error || !cast) && (
            <div
              className={cn(
                "pointer-events-none absolute inset-0 flex items-center justify-center p-4 text-center text-sm",
                error && "pointer-events-auto bg-background/80 text-red-600 dark:text-red-400",
              )}
            >
              {error ?? (
                <span className="flex items-center gap-1.5 rounded-md bg-background/80 px-2 py-1 text-muted-foreground">
                  <LoaderIcon className="size-3.5 animate-spin" />
                  Reading the recording…
                </span>
              )}
            </div>
          )}
        </div>
        {keysOpen && (
          <div ref={keysRef} className="flex w-64 shrink-0 flex-col overflow-y-auto border-l text-xs">
            {lines === null ? (
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
                  onClick={() => moveTo(l.at)}
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
          disabled={!cast}
          onClick={() => {
            if (!playing && atEnd) moveTo(0)
            setPlaying((p) => !p)
          }}
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
          disabled={!cast || total === 0}
          onValueChange={(v) => setScrub(v)}
          onValueCommitted={(v) => {
            setScrub(null)
            moveTo(v)
          }}
        />
      </div>
    </div>
  )
}

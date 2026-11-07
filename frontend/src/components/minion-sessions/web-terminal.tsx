import { useEffect, useRef, useState } from "react"
import { FitAddon } from "@xterm/addon-fit"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { RotateCcwIcon, XIcon, ZapIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import { tryRefresh } from "@/services/api-client"
import {
  CLOSE_REFUSED,
  CLOSE_UNKNOWN,
  SIGNAL_LABEL,
  endedLabel,
  readStatus,
  resizeFrame,
  signalFrame,
  terminalURL,
  type TerminalSignal,
} from "./terminal-socket"

// A web terminal on a terminal task (docs: contracts/minion-messages/
// tunnels.md, Terminal): one stream of the task, its process on the
// minion's host in a terminal of this one's size. xterm.js draws it; this
// file is a chunk of its own (lazy-web-terminal.tsx).

type Phase =
  | { kind: "connecting" }
  | { kind: "open"; stream: number | null }
  | { kind: "ended"; label: string; stream: number | null }
  | { kind: "refused"; why: string }

export interface WebTerminalProps {
  taskId: string
  /** Shown above the terminal: a connection that waits for calls makes it slow. */
  warning?: string | null
  onClose: () => void
  /** Opens another stream, once this one ended. */
  onRestart: () => void
}

export function WebTerminal({ taskId, warning, onClose, onRestart }: WebTerminalProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const [phase, setPhase] = useState<Phase>({ kind: "connecting" })

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 13,
      scrollback: 5000,
      theme: { background: "#0a0a0a" },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    fit.fit()
    const encoder = new TextEncoder()
    let disposed = false
    let opened = false
    let stream: number | null = null
    let retried = false
    let socket: WebSocket | null = null

    const connect = () => {
      const ws = new WebSocket(terminalURL(taskId, term.rows, term.cols))
      ws.binaryType = "arraybuffer"
      socket = ws
      socketRef.current = ws
      ws.onopen = () => {
        opened = true
        setPhase({ kind: "open", stream: null })
        term.focus()
      }
      ws.onmessage = (e: MessageEvent<ArrayBuffer | string>) => {
        if (typeof e.data !== "string") {
          term.write(new Uint8Array(e.data))
          return
        }
        const s = readStatus(e.data)
        if (s?.kind === "opened") {
          stream = s.stream
          setPhase({ kind: "open", stream })
        } else if (s?.kind === "ended") {
          setPhase({ kind: "ended", label: endedLabel(s), stream })
        }
      }
      ws.onclose = (e) => {
        if (disposed) return
        if (e.code === CLOSE_REFUSED || e.code === CLOSE_UNKNOWN) {
          setPhase({ kind: "refused", why: e.reason || "core did not open the terminal" })
          return
        }
        if (!opened && !retried) {
          // The upgrade failed: most likely the session's access token ran
          // out. Once refreshed, again.
          retried = true
          void tryRefresh().then((ok) => {
            if (disposed) return
            if (ok) connect()
            else setPhase({ kind: "refused", why: "could not reach core: sign in again" })
          })
          return
        }
        setPhase((p) => (p.kind === "ended" ? p : opened ? { kind: "ended", label: "Disconnected", stream } : { kind: "refused", why: "could not reach core" }))
      }
    }
    connect()

    const data = term.onData((d) => {
      if (socket?.readyState === WebSocket.OPEN) socket.send(encoder.encode(d))
    })
    const resize = term.onResize(({ rows, cols }) => {
      if (socket?.readyState === WebSocket.OPEN) socket.send(resizeFrame(rows, cols))
    })
    const observer = new ResizeObserver(() => {
      try {
        fit.fit()
      } catch {
        // Hidden: no size to fit.
      }
    })
    observer.observe(host)
    return () => {
      disposed = true
      observer.disconnect()
      data.dispose()
      resize.dispose()
      socket?.close()
      socketRef.current = null
      term.dispose()
    }
  }, [taskId])

  const signal = (s: TerminalSignal) => {
    const ws = socketRef.current
    if (ws?.readyState === WebSocket.OPEN) ws.send(signalFrame(s))
  }
  const live = phase.kind === "open"

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border bg-[#0a0a0a]">
      <div className="flex min-h-8 items-center gap-2 border-b bg-background px-2 py-1 text-xs">
        <span
          className={cn(
            "min-w-0 flex-1 truncate",
            phase.kind === "open" && "text-emerald-600 dark:text-emerald-400",
            phase.kind === "connecting" && "text-amber-600 dark:text-amber-400",
            phase.kind === "refused" && "text-red-600 dark:text-red-400",
            phase.kind === "ended" && "text-muted-foreground",
          )}
        >
          {phase.kind === "connecting" && "Connecting…"}
          {phase.kind === "open" && (phase.stream != null ? `Stream ${phase.stream}` : "Starting…")}
          {phase.kind === "ended" && `${phase.label}${phase.stream != null ? ` · stream ${phase.stream}` : ""}`}
          {phase.kind === "refused" && `Not opened: ${phase.why}`}
          {warning && <span className="ml-2 text-amber-600 dark:text-amber-400">{warning}</span>}
        </span>
        {live && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="xs" title="Send the process a signal">
                  <ZapIcon />
                  Signal
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-auto min-w-40">
              {(Object.keys(SIGNAL_LABEL) as TerminalSignal[]).map((s) => (
                <DropdownMenuItem key={s} onClick={() => signal(s)}>
                  {SIGNAL_LABEL[s]}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        {(phase.kind === "ended" || phase.kind === "refused") && (
          <Button variant="ghost" size="xs" title="Open another terminal: a new process" onClick={onRestart}>
            <RotateCcwIcon />
            New
          </Button>
        )}
        <Button variant="ghost" size="icon-xs" title="Close the terminal: its process is hung up" aria-label="Close the terminal" onClick={onClose}>
          <XIcon />
        </Button>
      </div>
      <div ref={hostRef} className="min-h-0 flex-1 p-1" />
    </div>
  )
}

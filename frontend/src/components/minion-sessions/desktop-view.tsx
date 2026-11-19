import { useEffect, useRef, useState, type FormEvent } from "react"
import { ClipboardIcon, ExpandIcon, KeyboardIcon, LoaderIcon, LogOutIcon, RotateCcwIcon, XIcon } from "lucide-react"
import Guacamole from "@/vendor/guacamole/guacamole-common"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import {
  CTRL_ALT_DEL,
  FRONT_RDP,
  FRONT_VNC,
  clipboardWays,
  connectData,
  desktopName,
  desktopURL,
  displayDPI,
  fitScale,
  followsPane,
  requiredLabel,
  requiredSecret,
  socketEndText,
  statusText,
  viewOnly,
  type DesktopSettings,
} from "./desktop-values"

// A desktop task's desktop, drawn (a chunk of its own: desktop-code.ts):
// Guacamole's client over core's socket, its display fitted to the frame
// (the remote size a monitor's, or the frame's where its settings say:
// RDP's "Follow the pane", and a terminal's), the
// keys typed while the frame has focus going to the server, its clipboard
// through a panel (the ways its settings let it go), and the login the
// server asks for when the task gave none. Each one opened is its own
// session with the server, drawn by the settings it opened with, and
// recorded by core (the task's Recordings tab). Closing an RDP desktop
// only disconnects it, the Windows session left signed in on the server:
// closing one connected reminds to sign out first, and so does the
// browser, leaving the page.

export interface DesktopViewProps {
  taskId: string
  /** Its front: rdp, vnc, ssh or telnet. */
  front: string
  /** What it opens with (DesktopSettingsDialog): core checks them. */
  settings: DesktopSettings
  /** Why it may be slow: the session's connection waits for the minion's calls. */
  warning?: string | null
  onClose: () => void
  /** Opens it again: a new session. */
  onRestart: () => void
}

type Phase = "connecting" | "connected" | "ended"

/** A session that ended with no error from guacd: its connection failed, or guacd did. */
const UNEXPLAINED_END = "guacd ended the session without saying why: it may have failed (its log says what happened)"

export function DesktopView({ taskId, front, settings, warning, onClose, onRestart }: DesktopViewProps) {
  const frameRef = useRef<HTMLDivElement>(null)
  const clientRef = useRef<Guacamole.Client | null>(null)
  const [phase, setPhase] = useState<Phase>("connecting")
  const [error, setError] = useState<string | null>(null)
  // The params the server asks for (a login), while it waits for them.
  const [required, setRequired] = useState<string[] | null>(null)
  const [clipOpen, setClipOpen] = useState(false)
  const [remoteClip, setRemoteClip] = useState("")
  const [localClip, setLocalClip] = useState("")
  // Closing asked: the reminder to sign out shows.
  const [closing, setClosing] = useState(false)

  useEffect(() => {
    const frame = frameRef.current
    if (!frame) return
    const tunnel = new Guacamole.WebSocketTunnel(desktopURL(taskId))
    const client = new Guacamole.Client(tunnel)
    clientRef.current = client
    const display = client.getDisplay()
    const element = display.getElement()
    frame.appendChild(element)
    const fit = () => display.scale(fitScale(frame.clientWidth, frame.clientHeight, display.getWidth(), display.getHeight()))
    display.onresize = fit
    client.onstatechange = (state) => {
      if (state === Guacamole.Client.State.CONNECTED) setPhase("connected")
      else if (state === Guacamole.Client.State.DISCONNECTED) {
        setPhase("ended")
        // An end guacd said nothing of (its error comes first): it failed.
        setError((said) => said ?? UNEXPLAINED_END)
      }
    }
    client.onerror = (status) => setError(statusText(status.code, status.message))
    // Guacamole's client ends a session on guacd's word (or core's) only:
    // a socket that ended unsaid (the network's end, or core's) would leave
    // the last frame on the screen, still. Its tunnel's close ends the
    // session too, saying why where no word came first.
    tunnel.onerror = (status) => {
      const opened = tunnel.state !== Guacamole.Tunnel.State.CONNECTING
      setError((said) => said ?? socketEndText(status.code, opened))
    }
    tunnel.onstatechange = (state) => {
      if (state === Guacamole.Tunnel.State.CLOSED) client.disconnect()
    }
    client.onrequired = (params) => setRequired(params)
    client.onclipboard = (stream, mimetype) => {
      if (!mimetype.startsWith("text/")) return
      const reader = new Guacamole.StringReader(stream)
      let text = ""
      reader.ontext = (t) => {
        text += t
      }
      reader.onend = () => {
        setRemoteClip(text)
        // Where the page may write the clipboard, the server's copy is
        // there too (guacd sends none where the settings keep it there).
        navigator.clipboard?.writeText(text).catch(() => {})
      }
    }
    const mouse = new Guacamole.Mouse(element)
    mouse.onEach(["mousedown", "mouseup", "mousemove"], (e) => client.sendMouseState(e.state, true))
    const keyboard = new Guacamole.Keyboard(frame)
    keyboard.onkeydown = (keysym) => {
      client.sendKeyEvent(1, keysym)
      return false
    }
    keyboard.onkeyup = (keysym) => client.sendKeyEvent(0, keysym)
    // A key whose release the page never sees stays down on the server:
    // Esc, which leaves full screen (the browser keeps its release), and any
    // held as the window loses focus. Each goes up then.
    const releaseKeys = () => keyboard.reset()
    document.addEventListener("fullscreenchange", releaseKeys)
    window.addEventListener("blur", releaseKeys)
    // In device pixels, at the density they have, as Guacamole's own
    // client asks: the display is shrunk back to the frame.
    const size = () => {
      const ratio = window.devicePixelRatio || 1
      return { width: frame.clientWidth * ratio, height: frame.clientHeight * ratio, dpi: displayDPI(ratio) }
    }
    // Opened once this effect stays: one undone at once (React's StrictMode
    // mounts each component twice in development) opens no session, which
    // guacd would have connected to the server for no one.
    let started = false
    const start = window.setTimeout(() => {
      started = true
      client.connect(connectData(size(), settings))
    })
    const follows = followsPane(front, settings)
    let resized = 0
    const observer = new ResizeObserver(() => {
      fit()
      // A fixed screen keeps its size: the pane scales it.
      if (!follows) return
      window.clearTimeout(resized)
      resized = window.setTimeout(() => {
        const s = size()
        if (s.width > 0 && s.height > 0) client.sendSize(Math.round(s.width), Math.round(s.height))
      }, 300)
    })
    observer.observe(frame)
    frame.focus()
    return () => {
      window.clearTimeout(start)
      document.removeEventListener("fullscreenchange", releaseKeys)
      window.removeEventListener("blur", releaseKeys)
      observer.disconnect()
      window.clearTimeout(resized)
      // Keys held go up, so none stays down on the server.
      keyboard.reset()
      keyboard.onkeydown = null
      keyboard.onkeyup = null
      client.onstatechange = null
      tunnel.onstatechange = null
      tunnel.onerror = null
      if (started) client.disconnect()
      element.remove()
      clientRef.current = null
    }
  }, [taskId, front, settings])

  const send = (keysyms: readonly number[]) => {
    const client = clientRef.current
    if (!client) return
    for (const k of keysyms) client.sendKeyEvent(1, k)
    for (const k of [...keysyms].reverse()) client.sendKeyEvent(0, k)
    frameRef.current?.focus()
  }

  const sendClipboard = (text: string) => {
    const client = clientRef.current
    if (!client) return
    const writer = new Guacamole.StringWriter(client.createClipboardStream("text/plain"))
    writer.sendText(text)
    writer.sendEnd()
  }

  const submitLogin = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const client = clientRef.current
    if (!client || !required) return
    const form = new FormData(e.currentTarget)
    for (const name of required) {
      const writer = new Guacamole.StringWriter(client.createArgumentValueStream("text/plain", name))
      writer.sendText(String(form.get(name) ?? ""))
      writer.sendEnd()
    }
    setRequired(null)
    frameRef.current?.focus()
  }

  const graphical = front === FRONT_RDP || front === FRONT_VNC
  const watched = viewOnly(settings)
  const clip = clipboardWays(settings)
  // A Windows session this closes would stay signed in: one the operator
  // can sign out of (not view only), still connected.
  const signedIn = front === FRONT_RDP && phase === "connected" && !watched
  const close = () => (signedIn ? setClosing(true) : onClose())
  useEffect(() => {
    if (!signedIn) return
    // The browser's own prompt: what it says is its own.
    const warn = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [signedIn])
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border">
      <div className="flex min-h-8 flex-wrap items-center gap-1 border-b px-2 py-1 text-xs">
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          {phase === "connecting" && <LoaderIcon className="size-3.5 shrink-0 animate-spin text-muted-foreground" />}
          <span className="font-medium">{desktopName(front)}</span>
          <span className={cn("truncate", error ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
            {error ?? (phase === "connecting" ? "connecting…" : phase === "connected" ? "connected" : "ended")}
          </span>
          {watched && !error && <span className="shrink-0 text-muted-foreground">· view only</span>}
          {phase !== "ended" && (
            <span className="shrink-0 text-muted-foreground" title="Core records this session: the task's Recordings tab plays it once it ends">
              · recorded
            </span>
          )}
          {warning && <span className="truncate text-amber-600 dark:text-amber-400">· {warning}</span>}
        </span>
        {graphical && !watched && phase === "connected" && (
          <Button variant="ghost" size="xs" title="Send Ctrl+Alt+Del" onClick={() => send(CTRL_ALT_DEL)}>
            <KeyboardIcon />
            Ctrl+Alt+Del
          </Button>
        )}
        {(clip.fromDesktop || clip.toDesktop) && (
          <Button
            variant={clipOpen ? "secondary" : "ghost"}
            size="xs"
            aria-pressed={clipOpen}
            title={
              clip.fromDesktop && clip.toDesktop
                ? "The desktop's clipboard, and text to send it"
                : clip.fromDesktop
                  ? "The desktop's clipboard (nothing of yours reaches it)"
                  : "Text to send the desktop's clipboard (nothing it copies reaches you)"
            }
            onClick={() => setClipOpen((o) => !o)}
          >
            <ClipboardIcon />
            Clipboard
          </Button>
        )}
        <Button
          variant="ghost"
          size="icon-xs"
          title="Full screen (Esc leaves it)"
          aria-label="Full screen"
          onClick={() => void frameRef.current?.requestFullscreen?.().then(() => frameRef.current?.focus())}
        >
          <ExpandIcon />
        </Button>
        {phase === "ended" && (
          <Button variant="ghost" size="xs" title="Open the desktop again: a new session" onClick={onRestart}>
            <RotateCcwIcon />
            Reconnect
          </Button>
        )}
        <Button variant="ghost" size="icon-xs" title="Close the desktop" aria-label="Close the desktop" onClick={close}>
          <XIcon />
        </Button>
      </div>
      {clipOpen && (clip.fromDesktop || clip.toDesktop) && (
        <div className={cn("grid gap-2 border-b p-2 text-xs", clip.fromDesktop && clip.toDesktop && "@2xl:grid-cols-2")}>
          {clip.fromDesktop && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">The desktop copied</span>
              <textarea readOnly value={remoteClip} rows={3} className="resize-none rounded-md border bg-muted/30 p-1.5 font-mono" />
            </label>
          )}
          {clip.toDesktop && (
            <form
              className="flex flex-col gap-1"
              onSubmit={(e) => {
                e.preventDefault()
                sendClipboard(localClip)
                frameRef.current?.focus()
              }}
            >
              <span className="text-muted-foreground">Text for the desktop to paste</span>
              <textarea
                value={localClip}
                onChange={(e) => setLocalClip(e.target.value)}
                rows={3}
                className="resize-none rounded-md border p-1.5 font-mono"
              />
              <span className="flex gap-1">
                <Button type="submit" size="xs" variant="secondary" disabled={localClip === ""}>
                  Send to the desktop
                </Button>
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  title="Read your clipboard (the browser asks first) and send it"
                  onClick={() =>
                    void navigator.clipboard
                      ?.readText()
                      .then((text) => {
                        setLocalClip(text)
                        sendClipboard(text)
                      })
                      .catch(() => {})
                  }
                >
                  Send your clipboard
                </Button>
              </span>
            </form>
          )}
        </div>
      )}
      <div className="relative flex min-h-0 flex-1">
        {/* The frame the display fits in; it takes the keys while focused,
            and keeps them from the console's own shortcuts. */}
        <div
          ref={frameRef}
          tabIndex={0}
          aria-label={`${desktopName(front)} desktop: click to type in it`}
          onMouseDown={() => frameRef.current?.focus()}
          onKeyDown={(e) => e.stopPropagation()}
          onKeyUp={(e) => e.stopPropagation()}
          className="flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-black outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        />
        {required && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/80">
            <form onSubmit={submitLogin} className="flex w-72 flex-col gap-2 rounded-md border bg-popover p-3 text-sm shadow-md">
              <span className="font-medium">The server asks for a login</span>
              {required.map((name, i) => (
                <label key={name} className="flex flex-col gap-1 text-xs">
                  <span className="text-muted-foreground">{requiredLabel(name)}</span>
                  <Input
                    name={name}
                    type={requiredSecret(name) ? "password" : "text"}
                    autoComplete="off"
                    autoFocus={i === 0}
                    className="h-7 text-xs md:text-xs"
                  />
                </label>
              ))}
              <span className="flex justify-end gap-1">
                <Button type="button" variant="ghost" size="xs" onClick={onClose}>
                  Cancel
                </Button>
                <Button type="submit" size="xs">
                  Log in
                </Button>
              </span>
            </form>
          </div>
        )}
        {closing && signedIn && (
          <div className="absolute inset-0 flex items-center justify-center bg-background/80 p-4">
            <div role="alertdialog" aria-label="Sign out first?" className="flex max-w-sm flex-col gap-2 rounded-md border bg-popover p-3 text-sm shadow-md">
              <span className="font-medium">Sign out first?</span>
              <span className="text-xs text-muted-foreground">
                Closing only disconnects: Windows keeps the session signed in, its programs running, until someone signs out.
              </span>
              <span className="flex flex-wrap justify-end gap-1">
                <Button
                  variant="ghost"
                  size="xs"
                  title="Send Ctrl+Alt+Del: its menu has Sign out"
                  onClick={() => {
                    setClosing(false)
                    send(CTRL_ALT_DEL)
                  }}
                >
                  <LogOutIcon />
                  Ctrl+Alt+Del
                </Button>
                <Button
                  variant="secondary"
                  size="xs"
                  autoFocus
                  onClick={() => {
                    setClosing(false)
                    frameRef.current?.focus()
                  }}
                >
                  Back to the desktop
                </Button>
                <Button variant="destructive" size="xs" onClick={onClose}>
                  Close anyway
                </Button>
              </span>
            </div>
          </div>
        )}
        {phase === "ended" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-background/80 p-4 text-center text-sm">
            <span>{error ?? "The desktop's session ended."}</span>
            <Button size="xs" variant="secondary" onClick={onRestart}>
              <RotateCcwIcon />
              Reconnect
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

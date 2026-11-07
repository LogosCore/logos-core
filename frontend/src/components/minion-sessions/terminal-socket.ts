// The web terminal's socket (core: GET /api/v1/minion-tasks/{id}/terminal;
// docs: contracts/minion-messages/tunnels.md, Terminal): binary frames are
// the process's bytes each way, text frames JSON. Ours: a new size, a
// signal, the end of the input. Core's: the stream's number once it opened,
// how it ended last.

/** The socket's URL for a terminal of rows and cols, on this page's host (nginx passes it to core). */
export function terminalURL(taskId: string, rows: number, cols: number, loc: Pick<Location, "protocol" | "host"> = window.location): string {
  const proto = loc.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${loc.host}/api/v1/minion-tasks/${encodeURIComponent(taskId)}/terminal?rows=${rows}&cols=${cols}`
}

/** A signal core sends the process: the minion's host maps it to its own (SIGINT, SIGTERM, SIGKILL; Ctrl-Break, ends on Windows). */
export type TerminalSignal = "interrupt" | "terminate" | "kill"

export const SIGNAL_LABEL: Record<TerminalSignal, string> = {
  interrupt: "Interrupt",
  terminate: "Terminate",
  kill: "Kill",
}

export function resizeFrame(rows: number, cols: number): string {
  return JSON.stringify({ rows, cols })
}

export function signalFrame(signal: TerminalSignal): string {
  return JSON.stringify({ signal })
}

/** What core says in a text frame. */
export type TerminalStatus =
  | { kind: "opened"; stream: number }
  | { kind: "ended"; end: string; exit: number | null; terminal: boolean | null }

export function readStatus(text: string): TerminalStatus | null {
  let v: unknown
  try {
    v = JSON.parse(text)
  } catch {
    return null
  }
  if (!v || typeof v !== "object") return null
  const o = v as Record<string, unknown>
  if (typeof o.end === "string" && o.end !== "") {
    return {
      kind: "ended",
      end: o.end,
      exit: typeof o.exit === "number" ? o.exit : null,
      terminal: typeof o.terminal === "boolean" ? o.terminal : null,
    }
  }
  if (typeof o.stream === "number" && o.stream > 0) return { kind: "opened", stream: o.stream }
  return null
}

/** Core's closes past the WebSocket's own: it did not open the terminal (4000: none runs there, or no stream is free; 4001: something else). */
export const CLOSE_REFUSED = 4000
export const CLOSE_UNKNOWN = 4001

/** How a terminal ended, for its bar: the process's exit, or how its stream went. */
export function endedLabel(s: { end: string; exit: number | null }): string {
  if (s.exit != null) {
    // 128 and a signal's number: killed by it, as shells say.
    return s.exit > 128 && s.exit < 160 ? `Exited ${s.exit} (signal ${s.exit - 128})` : `Exited ${s.exit}`
  }
  switch (s.end) {
    case "done":
      return "Ended"
    case "not_opened":
      return "Did not start"
    case "no_reply":
      return "The minion did not answer"
    case "task_ended":
      return "The task ended"
    default:
      return `Ended: ${s.end.replaceAll("_", " ")}`
  }
}

/** A connection kind over which every keystroke waits for the minion's next call: a terminal works, slowly. */
export function laggyKind(connKind: string | null | undefined): boolean {
  return connKind != null && connKind.endsWith("-call")
}

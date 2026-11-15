import Guacamole from "@/vendor/guacamole/guacamole-common"

// A recording's file as the tunnel Guacamole's player reads (its tunnel
// source: the player reports key events only from one): fetched with the
// page's session, its instructions handed over as they come, then closed.
// Guacamole's own StaticHTTPTunnel would do, but leaves a read that fails
// half way unsaid: its player waits for ever.

/** Status codes the player is told: core's refusal, or a read that failed. */
export const RECORDING_UNREADABLE = 0x0200

export function recordingTunnel(url: string): Guacamole.Tunnel {
  const tunnel = new Guacamole.Tunnel()
  let abort: AbortController | null = null
  const close = () => tunnel.setState(Guacamole.Tunnel.State.CLOSED)
  tunnel.sendMessage = () => {}
  tunnel.connect = () => {
    abort?.abort()
    const ac = new AbortController()
    abort = ac
    tunnel.setState(Guacamole.Tunnel.State.CONNECTING)
    const parser = new Guacamole.Parser()
    parser.oninstruction = (opcode, args) => tunnel.oninstruction?.(opcode, args)
    const decoder = new TextDecoder()
    void (async () => {
      try {
        const res = await fetch(url, { credentials: "same-origin", signal: ac.signal })
        if (!res.ok || !res.body) throw new Error(await refusal(res))
        tunnel.setState(Guacamole.Tunnel.State.OPEN)
        const reader = res.body.getReader()
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          parser.receive(decoder.decode(value, { stream: true }))
        }
        parser.receive(decoder.decode())
        if (abort === ac) close()
      } catch (err) {
        if (ac.signal.aborted) return
        tunnel.onerror?.(new Guacamole.Status(RECORDING_UNREADABLE, err instanceof Error ? err.message : String(err)))
        close()
      }
    })()
  }
  tunnel.disconnect = () => {
    abort?.abort()
    abort = null
    close()
  }
  return tunnel
}

/** What core said, refusing it: its error's message, else the status. */
async function refusal(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: unknown; message?: unknown }
    const said = body.error ?? body.message
    if (typeof said === "string" && said !== "") return said
  } catch {
    // Not core's JSON.
  }
  return `the recording could not be read (HTTP ${res.status})`
}

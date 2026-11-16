import { afterEach, describe, expect, test, vi } from "vitest"
import Guacamole from "@/vendor/guacamole/guacamole-common"
import { RECORDING_UNREADABLE, recordingTunnel } from "./recording-tunnel"

// A body whose chunks come one at a time, then fail when fail is set.
function body(chunks: Uint8Array[], fail?: Error): ReadableStream<Uint8Array> {
  let i = 0
  return new ReadableStream({
    pull(controller) {
      if (i < chunks.length) controller.enqueue(chunks[i++])
      else if (fail) controller.error(fail)
      else controller.close()
    },
  })
}

function watch(tunnel: Guacamole.Tunnel) {
  const seen = { instructions: [] as string[], states: [] as number[], errors: [] as string[] }
  tunnel.oninstruction = (opcode, args) => seen.instructions.push([opcode, ...args].join(","))
  tunnel.onerror = (status) => seen.errors.push(`${status.code}: ${status.message}`)
  const closed = new Promise<void>((resolve) => {
    tunnel.onstatechange = (state) => {
      seen.states.push(state)
      if (state === Guacamole.Tunnel.State.CLOSED) resolve()
    }
  })
  return { seen, closed }
}

const { CONNECTING, OPEN, CLOSED } = Guacamole.Tunnel.State

describe("recordingTunnel", () => {
  afterEach(() => vi.unstubAllGlobals())

  test("hands over the file's instructions as they come, cut anywhere, then closes", async () => {
    const text = new TextEncoder().encode("4.sync,3.100;4.name,6.Привет;3.key,2.97,1.1,3.150;")
    // Cut inside an instruction, and inside a character of two bytes.
    const cuts = [0, 7, 25, 26, text.length]
    const chunks = cuts.slice(1).map((end, i) => text.slice(cuts[i], end))
    const fetch = vi.fn(async () => new Response(body(chunks)))
    vi.stubGlobal("fetch", fetch)
    const tunnel = recordingTunnel("/api/v1/recordings/r1")
    const { seen, closed } = watch(tunnel)
    tunnel.connect()
    await closed
    expect(fetch).toHaveBeenCalledWith("/api/v1/recordings/r1", expect.objectContaining({ credentials: "same-origin" }))
    expect(seen.instructions).toEqual(["sync,100", "name,Привет", "key,97,1,150"])
    expect(seen.states).toEqual([CONNECTING, OPEN, CLOSED])
    expect(seen.errors).toEqual([])
  })

  test("core's refusal is the player's error", async () => {
    vi.stubGlobal("fetch", async () => Response.json({ error: "the desktop did not open: nothing was recorded" }, { status: 409 }))
    const tunnel = recordingTunnel("/x")
    const { seen, closed } = watch(tunnel)
    tunnel.connect()
    await closed
    expect(seen.errors).toEqual([`${RECORDING_UNREADABLE}: the desktop did not open: nothing was recorded`])
    expect(seen.states).toEqual([CONNECTING, CLOSED])
  })

  test("a read that fails half way says so, after what came", async () => {
    vi.stubGlobal("fetch", async () => new Response(body([new TextEncoder().encode("4.sync,1.5;")], new Error("network lost"))))
    const tunnel = recordingTunnel("/x")
    const { seen, closed } = watch(tunnel)
    tunnel.connect()
    await closed
    expect(seen.instructions).toEqual(["sync,5"])
    expect(seen.errors).toEqual([`${RECORDING_UNREADABLE}: network lost`])
  })

  test("disconnected: the read stops, nothing said", async () => {
    let aborted = false
    vi.stubGlobal("fetch", (_url: string, init: RequestInit) => {
      init.signal?.addEventListener("abort", () => (aborted = true))
      return new Promise<Response>(() => {})
    })
    const tunnel = recordingTunnel("/x")
    const { seen, closed } = watch(tunnel)
    tunnel.connect()
    tunnel.disconnect()
    await closed
    expect(aborted).toBe(true)
    expect(seen.errors).toEqual([])
    expect(seen.states).toEqual([CONNECTING, CLOSED])
  })
})

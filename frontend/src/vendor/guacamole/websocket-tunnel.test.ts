import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"
import Guacamole from "./guacamole-common"

// The desktop view ends a session as its tunnel closes
// (components/minion-sessions/desktop-view.tsx): Guacamole's client ends
// one on guacd's word only. These pin the tunnel's side of it: its
// socket's end is said, an error first where it is one, in the state the
// tunnel had (the view reads whether core had answered).

/** The tunnel's socket, driven by the test. */
class FakeSocket {
  static last: FakeSocket | undefined
  onopen: ((event: unknown) => void) | null = null
  onclose: ((event: { code: number; reason: string }) => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  constructor() {
    FakeSocket.last = this
  }
  send() {}
  close() {}
}

const { CONNECTING, OPEN, CLOSED, UNSTABLE } = Guacamole.Tunnel.State

describe("the vendored WebSocket tunnel", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    // Its timers are window's: node's own here.
    vi.stubGlobal("window", globalThis)
    vi.stubGlobal("WebSocket", FakeSocket)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  // A tunnel connecting, core's first word (the tunnel's id) heard where
  // answered: what it says from then on, and its socket.
  const connect = (answered: boolean) => {
    const tunnel = new Guacamole.WebSocketTunnel("ws://core/api/v1/minion-tasks/t1/desktop")
    const said: string[] = []
    tunnel.onerror = (status) => said.push(`error ${status.code.toString(16)} in ${tunnel.state}`)
    tunnel.onstatechange = (state) => said.push(`state ${state}`)
    tunnel.connect("width=800")
    const socket = FakeSocket.last
    if (!socket) throw new Error("the tunnel opened no socket")
    if (answered) socket.onmessage?.({ data: "0.,36.4f1c2b9e-8d3a-4c51-9e07-2b6d5a8c1f30;" })
    said.length = 0
    return { tunnel, said, socket }
  }

  test("a socket that dropped: an error, then closed", () => {
    const { said, socket } = connect(true)
    socket.onclose?.({ code: 1006, reason: "" })
    expect(said).toEqual([`error 207 in ${OPEN}`, `state ${CLOSED}`])
  })

  test("one that dropped before core answered: the same, still connecting", () => {
    const { said, socket } = connect(false)
    socket.onclose?.({ code: 1006, reason: "" })
    expect(said).toEqual([`error 207 in ${CONNECTING}`, `state ${CLOSED}`])
  })

  test("core's close after its error instruction: the status its reason says", () => {
    const { said, socket } = connect(true)
    socket.onclose?.({ code: 1000, reason: "776" })
    expect(said).toEqual([`error 308 in ${OPEN}`, `state ${CLOSED}`])
  })

  test("a normal close (core's, as guacd ended), or its own disconnect: closed, no error", () => {
    const one = connect(true)
    one.socket.onclose?.({ code: 1000, reason: "" })
    expect(one.said).toEqual([`state ${CLOSED}`])
    const other = connect(true)
    other.tunnel.disconnect()
    expect(other.said).toEqual([`state ${CLOSED}`])
  })

  test("core silent past its receive timeout: unstable, then an error, then closed", () => {
    const { said } = connect(true)
    vi.advanceTimersByTime(15_000)
    expect(said).toEqual([`state ${UNSTABLE}`, `error 202 in ${UNSTABLE}`, `state ${CLOSED}`])
  })
})

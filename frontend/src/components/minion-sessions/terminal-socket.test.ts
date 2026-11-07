import { describe, expect, it } from "vitest"
import { endedLabel, laggyKind, readStatus, resizeFrame, signalFrame, terminalURL } from "./terminal-socket"

describe("the web terminal's socket", () => {
  it("is on this page's host, ws or wss as the page", () => {
    expect(terminalURL("a b", 24, 80, { protocol: "https:", host: "ops:8443" })).toBe("wss://ops:8443/api/v1/minion-tasks/a%20b/terminal?rows=24&cols=80")
    expect(terminalURL("t", 1, 2, { protocol: "http:", host: "h" })).toBe("ws://h/api/v1/minion-tasks/t/terminal?rows=1&cols=2")
  })
  it("sends sizes and signals as JSON", () => {
    expect(JSON.parse(resizeFrame(50, 200))).toEqual({ rows: 50, cols: 200 })
    expect(JSON.parse(signalFrame("kill"))).toEqual({ signal: "kill" })
  })
  it("reads core's: the stream once open, how it ended last", () => {
    expect(readStatus('{"stream":7}')).toEqual({ kind: "opened", stream: 7 })
    expect(readStatus('{"end":"done","exit":0,"terminal":false}')).toEqual({ kind: "ended", end: "done", exit: 0, terminal: false })
    expect(readStatus('{"end":"reset"}')).toEqual({ kind: "ended", end: "reset", exit: null, terminal: null })
    expect(readStatus("nope")).toBeNull()
    expect(readStatus("{}")).toBeNull()
  })
  it("says how it ended", () => {
    expect(endedLabel({ end: "done", exit: 0 })).toBe("Exited 0")
    expect(endedLabel({ end: "done", exit: 137 })).toBe("Exited 137 (signal 9)")
    expect(endedLabel({ end: "not_opened", exit: null })).toBe("Did not start")
    expect(endedLabel({ end: "client_reset", exit: null })).toBe("Ended: client reset")
  })
  it("warns over a connection that waits for calls", () => {
    expect(laggyKind("reverse-call")).toBe(true)
    expect(laggyKind("bind-call")).toBe(true)
    expect(laggyKind("reverse-persistent")).toBe(false)
    expect(laggyKind(null)).toBe(false)
  })
})

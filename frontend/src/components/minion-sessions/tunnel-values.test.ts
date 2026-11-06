import { describe, expect, it } from "vitest"
import {
  frontLabel,
  hostPort,
  proxyState,
  proxyURL,
  proxychainsLine,
  streamState,
  tunnelVersion,
  type Tunnel,
  type TunnelStream,
} from "./tunnel-values"

const stream = (s: Partial<TunnelStream>): TunnelStream => ({
  stream: 1,
  client: "10.9.0.2:50122",
  host: "dc01",
  port: 445,
  udp: false,
  openedAt: "2026-11-06T10:00:00Z",
  state: "OPEN",
  result: "open",
  bound: "10.0.0.7:51234",
  msg: null,
  endedAt: null,
  end: null,
  up: 10,
  down: 20,
  ...s,
})

const tunnel = (t: Partial<Tunnel>): Tunnel => ({
  port: 41000,
  address: null,
  purpose: "socks_proxy",
  to: null,
  listening: true,
  open: 0,
  streams: 0,
  up: 0,
  down: 0,
  ...t,
})

describe("the proxy's address", () => {
  it("brackets an IPv6 address, as URLs and tools take it", () => {
    expect(hostPort("core.example.org", 41000)).toBe("core.example.org:41000")
    expect(hostPort("fe80::1", 41000)).toBe("[fe80::1]:41000")
    expect(hostPort("[fe80::1]", 41000)).toBe("[fe80::1]:41000")
    expect(proxyURL("10.0.0.1", 41003)).toBe("socks5h://10.0.0.1:41003")
    expect(proxychainsLine("fe80::1", 41000)).toBe("socks5 fe80::1 41000")
    expect(proxychainsLine("[fe80::1]", 41000)).toBe("socks5 fe80::1 41000")
  })
})

describe("a stream's state", () => {
  it("says where it stands, why it did not open and how it ended", () => {
    expect(streamState(stream({ state: "OPENING", result: null, bound: null }))).toMatchObject({ label: "Opening", tone: "pending" })
    expect(streamState(stream({}))).toEqual({ label: "Open", tone: "good", detail: "from 10.0.0.7:51234" })
    expect(streamState(stream({ state: "ENDED", result: "refused", bound: null, msg: "connection refused", end: "not_opened" }))).toEqual({
      label: "Not opened",
      tone: "bad",
      detail: "refused: nothing listens there (connection refused)",
    })
    expect(streamState(stream({ state: "ENDED", result: null, bound: null, end: "no_reply" }))).toMatchObject({
      label: "Not opened",
      detail: "the minion did not answer in time",
    })
    expect(streamState(stream({ state: "ENDED", end: "done" }))).toEqual({ label: "Closed", tone: "muted", detail: "closed by both ends" })
    expect(streamState(stream({ state: "ENDED", end: "client_reset" }))).toMatchObject({ label: "Closed", tone: "bad" })
    expect(streamState(stream({ state: "ENDED", result: "some_later", end: "not_opened" })).detail).toBe("some later")
  })
})

describe("the proxy's state", () => {
  it("is closed once its task ended, and tells that tools wait until the minion has the task", () => {
    expect(proxyState(tunnel({ listening: false }), "DONE").label).toBe("Closed")
    expect(proxyState(tunnel({}), "SENT")).toMatchObject({ label: "Listening", tone: "pending" })
    expect(proxyState(tunnel({}), "RUNNING")).toEqual({ label: "Listening", tone: "good", detail: null })
  })

  it("names what is in front of the port", () => {
    expect(frontLabel("socks_proxy")).toBe("SOCKS proxy")
    expect(frontLabel("port_forward")).toBe("Port forward")
    expect(frontLabel("http_proxy")).toBe("http proxy")
  })

  it("changes its version as the tunnel moves on", () => {
    expect(tunnelVersion(tunnel({}))).not.toBe(tunnelVersion(tunnel({ up: 1 })))
  })
})

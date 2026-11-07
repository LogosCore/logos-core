import { describe, expect, it } from "vitest"
import {
  frontLabel,
  hostPort,
  listenAddress,
  proxyState,
  stopIdleOf,
  reverseState,
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
  kept: 1000,
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

describe("reverse port forwards", () => {
  const reverse = (t: Partial<Tunnel>): Tunnel =>
    ({
      port: 0,
      address: null,
      purpose: "reverse_port_forward",
      to: "10.8.0.2:4444",
      bind: "0.0.0.0",
      listen: 8080,
      bound: null,
      udp: false,
      listening: false,
      open: 0,
      streams: 0,
      up: 0,
      down: 0,
      ...t,
    }) as Tunnel

  it("says what it is", () => {
    expect(frontLabel("reverse_port_forward")).toBe("Reverse port forward")
  })

  it("listens where the minion said, else where the task asked", () => {
    expect(listenAddress(reverse({}))).toBe("0.0.0.0:8080")
    expect(listenAddress(reverse({ bound: "10.0.0.7:8080" }))).toBe("10.0.0.7:8080")
    expect(listenAddress(reverse({ bind: "::", listen: 0 }))).toBe("[::]:0")
  })

  it("waits for the minion, listens once it said, and closes with its task", () => {
    expect(reverseState(reverse({}), "SENT")).toMatchObject({ label: "Waiting", tone: "pending" })
    expect(reverseState(reverse({}), "RUNNING")).toMatchObject({ label: "Waiting", tone: "pending" })
    expect(reverseState(reverse({ listening: true, bound: "0.0.0.0:8080" }), "RUNNING")).toEqual({
      label: "Listening",
      tone: "good",
      detail: "on the minion's host, at 0.0.0.0:8080",
    })
    expect(reverseState(reverse({}), "DONE")).toMatchObject({ label: "Closed", tone: "muted" })
  })

  it("names core's side in its streams", () => {
    expect(streamState(stream({ state: "OPENING", result: null, bound: null }), true).detail).toBe("core connects")
    expect(streamState(stream({ bound: "172.18.0.5:40000" }), true).detail).toBe("core from 172.18.0.5:40000")
    expect(streamState(stream({ state: "ENDED", end: "client_reset" }), true).detail).toBe("the destination aborted it")
    expect(streamState(stream({ state: "ENDED", end: "client_reset" })).detail).toBe("the tool aborted it")
    expect(streamState(stream({ state: "ENDED", end: "done", udp: true }), true).detail).toBe("idle: no datagram either way for two minutes")
    expect(streamState(stream({ state: "ENDED", end: "done", udp: true })).detail).toBe("closed by both ends")
  })
})

describe("stopIdleOf", () => {
  it("reads a tunnel's stop_idle, none or 0 as never", () => {
    expect(stopIdleOf({ stop_idle: "1h" })).toBe("1h")
    expect(stopIdleOf({ stop_idle: " 90m " })).toBe("90m")
    expect(stopIdleOf({ stop_idle: "0s" })).toBeNull()
    expect(stopIdleOf({ stop_idle: "0" })).toBeNull()
    expect(stopIdleOf({ stop_idle: 3600000 })).toBe("3600s")
    expect(stopIdleOf({ stop_idle: 0 })).toBeNull()
    expect(stopIdleOf({})).toBeNull()
    expect(stopIdleOf(null)).toBeNull()
  })
})

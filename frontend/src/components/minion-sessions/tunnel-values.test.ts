import { describe, expect, it } from "vitest"
import {
  capLabel,
  forwardTo,
  frontLabel,
  hostPort,
  httpProxyURL,
  parseCap,
  pathKind,
  listenAddress,
  proxyState,
  stopIdleOf,
  reverseState,
  proxyURL,
  proxychainsLine,
  socatCommand,
  streamDestination,
  streamState,
  terminalProgram,
  tunnelVersion,
  coreOrigin,
  guessPlatform,
  parseRoutes,
  vpnCommand,
  vpnState,
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
  front: "socks",
  reverse: false,
  to: null,
  path: null,
  source: null,
  tls: false,
  socksBind: false,
  cap: 0,
  listening: true,
  open: 0,
  streams: 0,
  kept: 1000,
  up: 0,
  down: 0,
  vpnClients: [],
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
    expect(proxychainsLine("10.0.0.1", 41001, "http")).toBe("http 10.0.0.1 41001")
    expect(httpProxyURL("fe80::1", 41001)).toBe("http://[fe80::1]:41001")
  })
})

describe("a forward's destination", () => {
  it("is its host and port, or its path, a pipe's when in \\\\", () => {
    expect(forwardTo({ to: "10.0.0.5:3389", path: null })).toBe("10.0.0.5:3389")
    expect(forwardTo({ to: null, path: "/var/run/docker.sock" })).toBe("/var/run/docker.sock")
    expect(pathKind("/var/run/docker.sock")).toBe("unix socket")
    expect(pathKind("@/tmp/.X11-unix/X0")).toBe("unix socket")
    expect(pathKind("\\\\dc01\\pipe\\lsarpc")).toBe("named pipe")
  })
})

describe("a tunnel's bandwidth cap", () => {
  it("says it, or none", () => {
    expect(capLabel(0)).toBe("none")
    expect(capLabel(512 * 1024)).toBe("512 KB/s each way")
  })

  it("reads as an operator types it", () => {
    for (const [text, value] of [
      ["", 0],
      ["none", 0],
      ["0", 0],
      ["1048576", 1048576],
      ["512k", 512 * 1024],
      ["2M", 2 * 1024 * 1024],
      ["1.5 MiB/s", 1.5 * 1024 * 1024],
      ["1g", 1024 ** 3],
    ] as const) {
      expect(parseCap(text)).toEqual({ ok: true, value })
    }
    expect(parseCap("fast").ok).toBe(false)
    expect(parseCap("-5").ok).toBe(false)
    expect(parseCap("3g").ok).toBe(false)
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
    expect(frontLabel("socks")).toBe("SOCKS proxy")
    expect(frontLabel("http")).toBe("HTTP proxy")
    expect(frontLabel("forward")).toBe("Port forward")
    expect(frontLabel("a_later_one")).toBe("a later one")
    expect(frontLabel("forward", true)).toBe("Reverse port forward")
    expect(frontLabel("socks", true)).toBe("Reverse socks")
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
      front: "forward",
      reverse: true,
      to: "10.8.0.2:4444",
      cap: 0,
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
    expect(frontLabel(reverse({}).front, true)).toBe("Reverse port forward")
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

describe("terminals", () => {
  it("names the front and what it runs", () => {
    expect(frontLabel("terminal")).toBe("Terminal")
    expect(terminalProgram({ program: null, args: null })).toBe("the host's shell")
    expect(terminalProgram({ program: "/bin/bash", args: "-l" })).toBe("/bin/bash -l")
    expect(socatCommand("ops", 41000)).toBe("socat file:`tty`,raw,echo=0 tcp:ops:41000")
  })

  it("says a process's stream: its program, a terminal or pipes, its exit", () => {
    const shell = stream({ kind: "process", host: "", port: 0, program: null, terminal: true, bound: null })
    expect(streamDestination(shell)).toBe("the host's shell")
    expect(streamState(shell)).toEqual({ label: "Open", tone: "good", detail: "in a terminal" })
    expect(streamState({ ...shell, terminal: false }).detail).toBe("on pipes: the host gave no terminal")
    expect(streamState({ ...shell, state: "ENDED", exit: 0, end: "done" })).toEqual({ label: "Exited 0", tone: "muted", detail: "closed by both ends" })
    expect(streamState({ ...shell, state: "ENDED", exit: 130, end: "done" })).toMatchObject({ label: "Exited 130", tone: "bad" })
    expect(streamDestination({ ...shell, program: "cmd.exe" })).toBe("cmd.exe")
  })

  it("says a BIND's stream: where the minion listens, who came", () => {
    const bind = stream({ kind: "accept", host: "10.0.0.9", port: 21, bound: "0.0.0.0:40001" })
    expect(streamDestination(bind)).toBe("BIND: listens at 0.0.0.0:40001")
    expect(streamState(bind).detail).toBe("listening at 0.0.0.0:40001")
    expect(streamState({ ...bind, came: "10.0.0.9:20" }).detail).toBe("10.0.0.9:20 came")
    expect(streamState(stream({ state: "ENDED", result: "in_use", end: "not_opened" })).detail).toBe("in use: something listens there already")
  })
})

describe("a VPN", () => {
  const client = {
    id: "00112233445566778899aabbccddeeff", name: "vpn: alice@kali", user: "alice", machine: "kali", os: "linux", arch: "amd64",
    version: "0.1.0", from: "203.0.113.5", since: "2026-11-08T10:00:00Z", connected: true, open: 1, streams: 3, up: 10, down: 20,
  }
  it("is open while its task runs, its machines counted", () => {
    expect(vpnState(tunnel({ front: "vpn", vpnClients: [client] }), "RUNNING")).toEqual({ label: "Open", tone: "good", detail: "1 machine connected" })
    expect(vpnState(tunnel({ front: "vpn" }), "SENT").tone).toBe("pending")
    expect(vpnState(tunnel({ front: "vpn", listening: false }), "SUCCEEDED").label).toBe("Closed")
    expect(frontLabel("vpn")).toBe("VPN")
  })
  it("gives the command for the machine's system", () => {
    expect(vpnCommand("https://logos.example.com", "lgv_k", "linux-amd64")).toBe("sudo logos-vpn https://logos.example.com lgv_k")
    expect(vpnCommand("https://logos.example.com", "lgv_k", "windows-amd64")).toBe(".\\logos-vpn.exe https://logos.example.com lgv_k")
    expect(guessPlatform("Mozilla/5.0 (Windows NT 10.0; Win64; x64)")).toBe("windows-amd64")
    expect(guessPlatform("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")).toBe("darwin-arm64")
    expect(guessPlatform("Mozilla/5.0 (X11; Linux aarch64)")).toBe("linux-arm64")
  })
  it("connects to the origin core's API is at", () => {
    expect(coreOrigin("/api/v1", "https://logos.example.com")).toBe("https://logos.example.com")
    expect(coreOrigin("http://localhost:8080/api/v1", "http://localhost:5173")).toBe("http://localhost:8080")
  })
  it("reads routes one a line or separated", () => {
    expect(parseRoutes("10.0.0.0/24\n fd00:1::/64, 172.16.0.0/12")).toEqual({ ok: true, value: ["10.0.0.0/24", "fd00:1::/64", "172.16.0.0/12"] })
    expect(parseRoutes("").ok).toBe(false)
    expect(parseRoutes("db.local").ok).toBe(false)
    expect(parseRoutes("10.0.0.5").ok).toBe(false)
  })
  it("reads the streams again as its machines change", () => {
    expect(tunnelVersion(tunnel({ vpnClients: [client] }))).not.toBe(tunnelVersion(tunnel({ vpnClients: [{ ...client, open: 2 }] })))
  })
})

import type { MinionTaskFieldsFragment, MinionTunnelStreamsQuery } from "@/graphql/gql/graphql"
import { formatBytes } from "@/lib/format-bytes"

// What a tunnel task's view says (docs: contracts/minion-messages/tunnels.md):
// the proxy's address in the forms tools take, where it stands, and how
// each of its streams went.

export type Tunnel = NonNullable<MinionTaskFieldsFragment["tunnel"]>
export type TunnelStream = MinionTunnelStreamsQuery["minionTunnelStreams"]["streams"][number]

/** host:port, an IPv6 address in brackets. */
export function hostPort(host: string, port: number): string {
  return host.includes(":") && !host.startsWith("[") ? `[${host}]:${port}` : `${host}:${port}`
}

/** The proxy as a URL for curl's -x, ALL_PROXY or a browser: socks5h, so names resolve at the proxy's end (on the minion's host). */
export function proxyURL(host: string, port: number): string {
  return `socks5h://${hostPort(host, port)}`
}

/** The proxy as a line of proxychains' [ProxyList]: socks5, or http for the HTTP proxy. */
export function proxychainsLine(host: string, port: number, kind: "socks5" | "http" = "socks5"): string {
  return `${kind} ${host.replace(/^\[|\]$/g, "")} ${port}`
}

/** The HTTP proxy as a URL: for HTTP_PROXY and HTTPS_PROXY, curl -x, or a browser. */
export function httpProxyURL(host: string, port: number): string {
  return `http://${hostPort(host, port)}`
}

/** A reverse SOCKS proxy as a URL for a tool on the minion's side: its user and password in it, names resolved at core's end. */
export function reverseSocksURL(address: string, user: string, password: string): string {
  return `socks5h://${encodeURIComponent(user)}:${encodeURIComponent(password)}@${address}`
}

/** Why the minion could not connect, by its reply's result. */
export const RESULT_LABEL: Record<string, string> = {
  open: "open",
  refused: "refused: nothing listens there",
  host_unreachable: "host unreachable, or its name does not resolve",
  net_unreachable: "network unreachable",
  timed_out: "timed out connecting",
  not_allowed: "not allowed: as many streams open as the task takes",
  failed: "failed",
  unsupported: "unsupported: the minion does not take this open",
  not_found: "not found: no such socket or pipe",
  denied: "denied: the minion may not open it",
}

/** How a stream ended. */
export const END_LABEL: Record<string, string> = {
  done: "closed by both ends",
  not_opened: "not opened",
  no_reply: "the minion did not answer in time",
  client_reset: "the tool aborted it",
  reset: "reset by the destination, or the minion",
  lost: "the minion no longer had it",
  task_ended: "the task ended",
  core_stopped: "core stopped",
}

export type Tone = "pending" | "good" | "muted" | "bad"

/** How a reverse port forward's stream ended, where it differs: core's end of it is the destination. */
export const REVERSE_END_LABEL: Record<string, string> = {
  client_reset: "the destination aborted it",
  reset: "reset by who connected to the minion, or the minion",
}

/**
 * A stream's state for its row: a word, its tone, and what to say of it. A
 * reverse port forward's (reverse) is one core connects, from its own
 * network.
 */
export function streamState(s: TunnelStream, reverse = false): { label: string; tone: Tone; detail: string | null } {
  const endLabel = (end: string) =>
    // A UDP reverse stream has no ends to close: done is its idling out.
    (reverse && s.udp && end === "done" ? "idle: no datagram either way for two minutes" : undefined) ??
    (reverse ? REVERSE_END_LABEL[end] : undefined) ??
    END_LABEL[end]
  if (s.state === "OPENING") return { label: "Opening", tone: "pending", detail: reverse ? "core connects" : "the minion connects" }
  if (s.state === "OPEN") return { label: "Open", tone: "good", detail: s.bound ? (reverse ? `core from ${s.bound}` : `from ${s.bound}`) : null }
  if (s.result && s.result !== "open") {
    const why = RESULT_LABEL[s.result] ?? s.result.replaceAll("_", " ")
    return { label: "Not opened", tone: "bad", detail: s.msg ? `${why} (${s.msg})` : why }
  }
  if (!s.result) {
    return { label: "Not opened", tone: "bad", detail: s.end ? (endLabel(s.end) ?? s.end) : null }
  }
  const how = s.end ? (endLabel(s.end) ?? s.end.replaceAll("_", " ")) : null
  return { label: "Closed", tone: s.end === "done" ? "muted" : "bad", detail: how }
}

/** Where the proxy stands, as its task does. */
export function proxyState(t: Tunnel, taskState: string): { label: string; tone: Tone; detail: string | null } {
  if (!t.listening) return { label: "Closed", tone: "muted", detail: "its task ended: the port went back to core" }
  if (taskState === "QUEUED" || taskState === "SENT") {
    return { label: "Listening", tone: "pending", detail: "tools wait until the minion has the task" }
  }
  return { label: "Listening", tone: "good", detail: null }
}

/** What core puts in front of a tunnel (its task's front): a SOCKS proxy, an HTTP proxy, or a port forward; a reverse tunnel's minion listens on its host, and core forwards each connection or speaks SOCKS on it. */
export const FRONT_SOCKS = "socks"
export const FRONT_HTTP = "http"
export const FRONT_FORWARD = "forward"

/** What a tunnel is, for a person: its front, and which way it goes. */
export function frontLabel(front: string, reverse = false): string {
  if (reverse) return front === FRONT_SOCKS ? "Reverse SOCKS" : "Reverse port forward"
  switch (front) {
    case FRONT_FORWARD:
      return "Port forward"
    case FRONT_SOCKS:
      return "SOCKS proxy"
    case FRONT_HTTP:
      return "HTTP proxy"
  }
  return front.replaceAll("_", " ")
}

/** What a forward's path is: a named pipe when it starts "\\\\" (\\\\.\\pipe\\name), else a unix socket (protocol.TunnelPathKind). */
export function pathKind(path: string): "named pipe" | "unix socket" {
  return path.startsWith("\\\\") ? "named pipe" : "unix socket"
}

/** Where a forward goes: its host and port, or its path. */
export function forwardTo(t: Pick<Tunnel, "to" | "path">): string | null {
  return t.path ?? t.to ?? null
}

/** The most a cap may be: GraphQL's Int, 2 GiB a second. */
export const MAX_CAP = 2 ** 31 - 1

/** A tunnel's bandwidth cap, for a person: bytes a second each way, or none. */
export function capLabel(cap: number): string {
  return cap > 0 ? `${formatBytes(cap)}/s each way` : "none"
}

/**
 * A cap as an operator types it: bytes a second, with k, M or G (1024s),
 * "B" and "/s" optional ("512k", "2 MB/s"); empty, 0 or "none" for none.
 */
export function parseCap(text: string): { ok: true; value: number } | { ok: false; error: string } {
  const t = text.trim().toLowerCase()
  if (t === "" || t === "none") return { ok: true, value: 0 }
  const m = /^(\d+(?:\.\d+)?)\s*([kmg]?)i?b?(?:\/s)?$/.exec(t)
  if (!m) return { ok: false, error: "Bytes a second: 512k, 2M, or 0 for none" }
  const value = Math.round(Number(m[1]) * 1024 ** "_kmg".indexOf(m[2] || "_"))
  if (value > MAX_CAP) return { ok: false, error: "2 GiB a second at most" }
  return { ok: true, value }
}

/** Where a reverse port forward's minion listens: where it said, else where its task asked. */
export function listenAddress(t: Tunnel): string {
  return t.bound ?? hostPort(t.bind ?? "?", t.listen ?? 0)
}

/** Where a reverse port forward stands: the minion listens once it has the task, and says where. */
export function reverseState(t: Tunnel, taskState: string): { label: string; tone: Tone; detail: string | null } {
  if (t.listening) return { label: "Listening", tone: "good", detail: t.bound ? `on the minion's host, at ${t.bound}` : null }
  if (taskState === "QUEUED" || taskState === "SENT" || taskState === "RUNNING") {
    return { label: "Waiting", tone: "pending", detail: "the minion listens once it has the task, and says where" }
  }
  return { label: "Closed", tone: "muted", detail: "its task ended: the minion no longer listens" }
}

/**
 * A tunnel task's stop_idle as its args say it ("1h"): null for none, or 0
 * (never). Core stops the task once no stream has been open that long.
 */
export function stopIdleOf(args: unknown): string | null {
  const v = args && typeof args === "object" ? (args as Record<string, unknown>).stop_idle : undefined
  if (typeof v === "number") return v > 0 ? `${Math.round(v / 1000)}s` : null
  if (typeof v !== "string") return null
  const s = v.trim()
  return s === "" || /^0+(\.0+)?[a-zµ]*$/i.test(s) ? null : s
}

/** The version a task's live updates bring: the streams list is read again when it changes. */
export function tunnelVersion(t: Tunnel): string {
  return [t.listening, t.open, t.streams, t.up, t.down, t.bound ?? ""].join(":")
}

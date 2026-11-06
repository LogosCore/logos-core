import type { MinionTaskFieldsFragment, MinionTunnelStreamsQuery } from "@/graphql/gql/graphql"

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

/** The proxy as a line of proxychains' [ProxyList]. */
export function proxychainsLine(host: string, port: number): string {
  return `socks5 ${host.replace(/^\[|\]$/g, "")} ${port}`
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

/** A stream's state for its row: a word, its tone, and what to say of it. */
export function streamState(s: TunnelStream): { label: string; tone: Tone; detail: string | null } {
  if (s.state === "OPENING") return { label: "Opening", tone: "pending", detail: "the minion connects" }
  if (s.state === "OPEN") return { label: "Open", tone: "good", detail: s.bound ? `from ${s.bound}` : null }
  if (s.result && s.result !== "open") {
    const why = RESULT_LABEL[s.result] ?? s.result.replaceAll("_", " ")
    return { label: "Not opened", tone: "bad", detail: s.msg ? `${why} (${s.msg})` : why }
  }
  if (!s.result) {
    return { label: "Not opened", tone: "bad", detail: s.end ? (END_LABEL[s.end] ?? s.end) : null }
  }
  const how = s.end ? (END_LABEL[s.end] ?? s.end.replaceAll("_", " ")) : null
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

/** What core puts in front of a tunnel's port (its task's tunnel action): a port forward, or else a SOCKS proxy. */
export const PORT_FORWARD = "port_forward"
export const SOCKS_PROXY = "socks_proxy"

/** What core puts in front of a tunnel's port, for a person. */
export function frontLabel(purpose: string): string {
  return purpose === PORT_FORWARD ? "Port forward" : purpose === SOCKS_PROXY ? "SOCKS proxy" : purpose.replaceAll("_", " ")
}

/** The version a task's live updates bring: the streams list is read again when it changes. */
export function tunnelVersion(t: Tunnel): string {
  return [t.listening, t.open, t.streams, t.up, t.down].join(":")
}

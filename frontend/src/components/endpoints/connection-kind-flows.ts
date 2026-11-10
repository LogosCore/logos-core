// How messages move for each connection kind between a minion, its channel
// and core (docs: contracts/channel-core-connections), as the kind picker's
// diagram plays it: a loop of events (a message along a path, a task queued
// at core, a note under a link) and the links' state over time, with the
// steps the loop walks through. Times are seconds into the loop.

/** The three places: the minion, the channel, core. */
export type Node = "m" | "c" | "k"

/** A link between two places: minion-channel or channel-core. */
export type Segment = "mc" | "ck"

/**
 * How a link looks: open (a connection that stays), flash (a request going
 * through it now), none (nothing open between messages).
 */
export type LinkState = "open" | "flash" | "none"

export interface FlowEvent {
  at: number
  /** Where a message goes, place by place ("mck": minion to core). */
  path?: string
  label?: string
  /** Core sends it at once, without being asked. */
  push?: boolean
  /** A small one (a ping). */
  tiny?: boolean
  /** A task queued at core, shown until `until`. */
  badge?: string
  until?: number
  /** A note under a link for a moment. */
  mark?: string
  /** The step it belongs to (an index into steps). */
  step?: number
}

export interface KindFlow {
  /** What the kind is, for people, in two parts: "Calls in", "a request each exchange". */
  label: string
  sub: string
  /** One sentence on what the kind does, under the kind picker. */
  summary: string
  period: number
  events: FlowEvent[]
  links: (t: number, events: readonly FlowEvent[]) => Record<Segment, LinkState>
  steps: string[]
}

/** Seconds a message takes across one link. */
export const HOP = 0.55

/** The links a path crosses, in order: "mck" is mc, ck. */
export function hops(path: string): string[] {
  const out: string[] = []
  for (let i = 0; i + 1 < path.length; i++) out.push(path[i] + path[i + 1])
  return out
}

/** The link a hop crosses, either way. */
export function segmentOf(hop: string): Segment {
  return hop === "mc" || hop === "cm" ? "mc" : "ck"
}

/** Whether a message is crossing seg at t (with a moment either side). */
export function crossing(events: readonly FlowEvent[], t: number, seg: Segment): boolean {
  return events.some(
    (e) =>
      e.path !== undefined &&
      hops(e.path).some((h, i) => segmentOf(h) === seg && t >= e.at + i * HOP - 0.05 && t <= e.at + (i + 1) * HOP + 0.15),
  )
}

/** The step the loop is at, at t: the last event's that has begun. */
export function stepAt(flow: KindFlow, t: number): number {
  let step = 0
  for (const e of flow.events) if (t >= e.at && e.step !== undefined) step = e.step
  return step
}

export const KIND_FLOWS: Record<string, KindFlow> = {
  "reverse-call": {
    label: "Calls in",
    sub: "a request each exchange",
    summary: "The minion checks in on its schedule; a task waits for its next check-in.",
    period: 9,
    links: (t, ev) => ({ mc: crossing(ev, t, "mc") ? "flash" : "none", ck: crossing(ev, t, "ck") ? "flash" : "none" }),
    events: [
      { at: 0.3, path: "mck", label: "check-in", step: 0 },
      { at: 1.5, path: "kcm", label: "empty reply", step: 1 },
      { at: 2.8, badge: "task queued", until: 5.6, step: 2 },
      { at: 4.6, path: "mck", label: "check-in", step: 3 },
      { at: 5.8, path: "kcm", label: "reply + task", step: 3 },
      { at: 7.2, path: "mck", label: "result", step: 4 },
    ],
    steps: [
      "The minion checks in on its schedule: a request through the channel to core.",
      "Core has nothing for it: an empty reply. Nothing stays open.",
      "An operator queues a task. Core cannot reach the minion: the task waits.",
      "At the next check-in the task goes back in the reply.",
      "The result goes with the minion's next request.",
    ],
  },
  "reverse-persistent": {
    label: "Stays connected",
    sub: "core sends at once",
    summary: "The minion keeps a connection open, so core sends it a task at once.",
    period: 9,
    links: (t) => ({ mc: t > 0.3 && t < 8.6 ? "open" : "none", ck: t > 0.8 && t < 8.6 ? "open" : "none" }),
    events: [
      { at: 0.3, mark: "WebSocket opens", step: 0 },
      { at: 0.9, path: "mck", label: "check-in", step: 1 },
      { at: 2.1, path: "kcm", label: "reply", step: 1 },
      { at: 3.3, badge: "task queued", until: 3.7, step: 2 },
      { at: 3.5, path: "kcm", label: "task, pushed", push: true, step: 2 },
      { at: 4.8, path: "mck", label: "ack + result", step: 3 },
      { at: 6.2, path: "mc", label: "ping", tiny: true, step: 4 },
      { at: 6.7, path: "cm", label: "pong", tiny: true, step: 4 },
    ],
    steps: [
      "The minion opens a WebSocket to the channel, which opens a socket to core.",
      "Its first message (a check-in) ties the socket to its session.",
      "An operator queues a task: core pushes it at once.",
      "The minion acks it and sends the result over the same connection.",
      "Pings keep it alive; no scheduled check-ins are needed.",
    ],
  },
  "bind-call": {
    label: "Called",
    sub: "core calls the minion",
    summary: "Core calls the minion when it has a task; the minion never dials out.",
    period: 9,
    links: (t, ev) => ({ mc: crossing(ev, t, "mc") ? "flash" : "none", ck: "open" }),
    events: [
      { at: 0.3, badge: "task queued", until: 1.0, step: 0 },
      { at: 0.6, path: "kc", label: "call target", push: true, step: 1 },
      { at: 1.4, path: "cm", label: "request + task", push: true, step: 1 },
      { at: 2.4, path: "mck", label: "reply", step: 2 },
      { at: 4.9, path: "kc", label: "call", push: true, step: 3 },
      { at: 5.7, path: "cm", label: "request", push: true, step: 3 },
      { at: 6.7, path: "mck", label: "result", step: 3 },
    ],
    steps: [
      "The minion listens (a web page, a service); it never dials out.",
      "Core asks the channel, over its control link, to call it; the request carries the task.",
      "The minion only answers: its reply goes back to core.",
      "Core calls again when it has something, or on its schedule, for the result.",
    ],
  },
  "bind-persistent": {
    label: "Reached",
    sub: "core opens the connection",
    summary: "Core opens a connection to the minion and sends tasks over it while it is open.",
    period: 9,
    links: (t) => ({ mc: t > 1.6 && t < 8.2 ? "open" : "none", ck: "open" }),
    events: [
      { at: 0.4, path: "kc", label: "open", push: true, step: 0 },
      { at: 1.1, path: "cm", label: "dial", push: true, step: 0 },
      { at: 1.9, path: "mck", label: "hello", step: 1 },
      { at: 3.3, badge: "task queued", until: 3.7, step: 2 },
      { at: 3.5, path: "kcm", label: "task, pushed", push: true, step: 2 },
      { at: 4.8, path: "mck", label: "result", step: 2 },
      { at: 6.6, path: "kc", label: "close", push: true, step: 3 },
    ],
    steps: [
      "Core asks the channel to open a connection; the channel dials the minion.",
      "The minion says hello over it: the connection serves its session.",
      "While it is open core pushes tasks at once, and results come back over it.",
      "Core closes it when it is done, and opens it again when it needs to.",
    ],
  },
}

/** The kinds the protocol defines, in its order. */
export const KNOWN_KINDS = ["reverse-call", "reverse-persistent", "bind-call", "bind-persistent"]

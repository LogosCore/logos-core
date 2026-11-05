import type { MinionTargetFieldsFragment } from "@/graphql/gql/graphql"

// Targets' states, as core keeps them (models.Target*): what the latest call
// to a target came to.

export type Target = MinionTargetFieldsFragment

export const TARGET_STATES = [
  "new",
  "connected",
  "unreachable",
  "not_the_minion",
  "refused",
  "stopped",
  "no_channel",
  "needs_editing",
  "connecting",
  "not_connected",
] as const

export const TARGET_STATE_LABEL: Record<string, string> = {
  new: "New",
  connected: "Connected",
  unreachable: "Unreachable",
  not_the_minion: "Not the minion",
  refused: "Refused",
  stopped: "Stopped",
  no_channel: "No channel",
  needs_editing: "Needs editing",
  connecting: "Connecting",
  not_connected: "Not connected",
}

/** What each state means, for its tooltip. */
export const TARGET_STATE_HELP: Record<string, string> = {
  new: "Added: the first call is on its way.",
  connected: "The minion answered: its hello opened the session.",
  unreachable: "Nothing answered (DNS, refused, timeout, TLS). Core calls again: 1 minute, 5, 30, then hourly, for a day.",
  not_the_minion: "Something answered, but not the minion (an HTTP 404, a WAF's page, another server). Core does not retry: Check now once it is fixed.",
  refused:
    "Refused: the login or the host key was not taken, the minion answered with an error, or its build or connection is gone. Core calls it again only on Check now or after an edit.",
  stopped: "Unreachable for a day: core stopped retrying. Check now calls it again.",
  no_channel: "No instance of the target's channel has its control link open.",
  needs_editing:
    "Its values no longer fit the channel's target fields (one it now requires is missing, one is gone or changed). Core does not call it until it is edited.",
  connecting: "Core asked its channel to open the connection: the channel dials it.",
  not_connected:
    "Its connection is closed on purpose (idle, disconnected, or the minion hung up): core opens it again as its policy says.",
}

export const TARGET_STATE_STYLE: Record<string, string> = {
  new: "border-sky-600/40 text-sky-700 dark:text-sky-400",
  connected: "border-emerald-600/40 text-emerald-700 dark:text-emerald-400",
  unreachable: "border-amber-600/40 text-amber-700 dark:text-amber-400",
  not_the_minion: "border-destructive/40 text-destructive",
  refused: "border-destructive/40 text-destructive",
  stopped: "border-muted-foreground/30 text-muted-foreground",
  no_channel: "border-amber-600/40 text-amber-700 dark:text-amber-400",
  needs_editing: "border-amber-600/40 text-amber-700 dark:text-amber-400",
  connecting: "border-sky-600/40 text-sky-700 dark:text-sky-400",
  not_connected: "border-muted-foreground/30 text-muted-foreground",
}

/** A target's values for a form, by field key: what is set, nothing else. */
export function targetValues(t: Pick<Target, "values">): Record<string, unknown> {
  return t.values && typeof t.values === "object" && !Array.isArray(t.values) ? (t.values as Record<string, unknown>) : {}
}

/**
 * The state a target shows: needs editing as soon as its values no longer
 * fit the channel's fields, before core's next call finds it.
 */
export function shownState(t: Pick<Target, "state" | "problems">): string {
  return t.problems.length > 0 ? "needs_editing" : t.state
}

/** A target's problems as one line: "token: required; url: …". */
export function problemsText(t: Pick<Target, "problems">): string {
  return t.problems.map((p) => (p.key ? `${p.key}: ${p.message}` : p.message)).join("; ")
}

/** A bind-persistent target's policies: who opens its connection. */
export const TARGET_POLICIES = ["on-demand", "manual", "always"] as const

export const TARGET_POLICY_LABEL: Record<string, string> = {
  "on-demand": "On demand",
  manual: "Manual",
  always: "Always",
}

export const TARGET_POLICY_HELP: Record<string, string> = {
  "on-demand": "Core opens it when the session has something to send, and closes it after 10 minutes without an exchange.",
  manual: "An operator opens it (Connect) and closes it (Disconnect); it closes after an hour without an exchange, as a safety net. Tasks wait meanwhile.",
  always: "Core keeps it open, opening it again whenever it drops.",
}

/** Whether a target's minion is reached over a connection kept open (bind-persistent). */
export function isPersistent(t: Pick<Target, "kind">): boolean {
  return t.kind === "bind-persistent"
}

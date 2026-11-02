import { parseGoDuration } from "@/components/modules/management/field-values"
import { childPointer, isRecord } from "@/lib/json-edit"

// A build's failover settings (docs: contracts/channel-core-connections,
// Failover; protocol.BuildFailover): how its minion moves between its
// connections on its own when the one in use keeps failing. Core sends them
// to the factory with the build; a factory whose schema declares failover
// builds them in, any other may ignore them. Keys are the wire's, so the
// form, the JSON tab and the build's record share them.

/** The build's policy, over its connections in their order. */
export interface FailoverPolicy {
  /** The minion moves on its own; off, only an operator's switch moves it. */
  enabled: boolean
  /** Failed exchanges in a row on a connection before the minion moves off it. */
  fail_after: number
  /** How long the connection must also have been failing ("0s": the count alone). */
  fail_for: string
  /** After the last connection, the first again; off, the minion stays on the last. */
  wrap: boolean
  /** The wait after a round where every connection failed. */
  cycle_pause: string
  /** How long away from the first connection before trying it again, once ("0s": never). */
  failback_after: string
}

/** A connection's own failover settings; unset keys take the defaults. */
export interface ConnectionFailover {
  /** Failover may move the minion here; unset: yes, but no for bind-persistent. */
  auto?: boolean
  /** Failures in a row before the minion moves off this one; unset: the build's. */
  fail_after?: number
}

export const DEFAULT_FAILOVER: FailoverPolicy = {
  enabled: true,
  fail_after: 5,
  fail_for: "0s",
  wrap: true,
  cycle_pause: "5m",
  failback_after: "1h",
}

export const FAILOVER_KEYS = Object.keys(DEFAULT_FAILOVER) as (keyof FailoverPolicy)[]
const CONNECTION_FAILOVER_KEYS = ["auto", "fail_after"]

const MAX_FAIL_AFTER = 1000
const HOUR_NS = 3_600_000_000_000
/** The longest each duration may be, in nanoseconds (core's limits). */
const DURATION_MAX: Record<"fail_for" | "cycle_pause" | "failback_after", [number, string]> = {
  fail_for: [24 * HOUR_NS, "24h"],
  cycle_pause: [24 * HOUR_NS, "24h"],
  failback_after: [168 * HOUR_NS, "168h"],
}

/** Whether failover may move a minion to a connection of connKind with these settings. */
export function failoverAuto(connKind: string, f: ConnectionFailover | undefined): boolean {
  return f?.auto ?? connKind !== "bind-persistent"
}

/** Adds the policy's problems to errors, keyed as core keys them ("failover.fail_for"). */
export function failoverErrors(f: FailoverPolicy, errors: Record<string, string>) {
  if (!Number.isInteger(f.fail_after) || f.fail_after < 1 || f.fail_after > MAX_FAIL_AFTER) {
    errors["failover.fail_after"] = `A whole number, 1 to ${MAX_FAIL_AFTER}`
  }
  for (const key of ["fail_for", "cycle_pause", "failback_after"] as const) {
    const ns = parseGoDuration(f[key])
    const [max, maxText] = DURATION_MAX[key]
    if (ns === null) errors[`failover.${key}`] = 'A duration, e.g. "0s", "5m", "1h"'
    else if (ns < 0 || ns > max) errors[`failover.${key}`] = `0s to ${maxText}`
  }
}

/** Adds a connection's failover problems to errors (at: "connections[1]"). */
export function connectionFailoverErrors(at: string, f: ConnectionFailover | undefined, errors: Record<string, string>) {
  const n = f?.fail_after
  if (n !== undefined && (!Number.isInteger(n) || n < 1 || n > MAX_FAIL_AFTER)) {
    errors[`${at}.failover.fail_after`] = `A whole number, 1 to ${MAX_FAIL_AFTER}, or empty for the build's`
  }
}

/** A connection's settings as sent: none when they set nothing. */
export function connectionFailoverInput(f: ConnectionFailover | undefined): ConnectionFailover | null {
  if (!f || (f.auto === undefined && f.fail_after === undefined)) return null
  return { ...(f.auto !== undefined ? { auto: f.auto } : {}), ...(f.fail_after !== undefined ? { fail_after: f.fail_after } : {}) }
}

/** A build record's policy (its failover JSON); the defaults where it says none. */
export function recordFailover(raw: unknown): FailoverPolicy {
  const out = { ...DEFAULT_FAILOVER }
  if (!isRecord(raw)) return out
  if (typeof raw.enabled === "boolean") out.enabled = raw.enabled
  if (typeof raw.fail_after === "number") out.fail_after = raw.fail_after
  if (typeof raw.fail_for === "string") out.fail_for = raw.fail_for
  if (typeof raw.wrap === "boolean") out.wrap = raw.wrap
  if (typeof raw.cycle_pause === "string") out.cycle_pause = raw.cycle_pause
  if (typeof raw.failback_after === "string") out.failback_after = raw.failback_after
  return out
}

/** A recorded connection's own settings (its failover JSON); undefined for none. */
export function recordConnectionFailover(raw: unknown): ConnectionFailover | undefined {
  if (!isRecord(raw)) return undefined
  const out: ConnectionFailover = {}
  if (typeof raw.auto === "boolean") out.auto = raw.auto
  if (typeof raw.fail_after === "number" && raw.fail_after > 0) out.fail_after = raw.fail_after
  return out.auto === undefined && out.fail_after === undefined ? undefined : out
}

type At = (path: string, message: string) => void

/** The JSON tab's "failover" read back: the defaults where it leaves keys out. */
export function readFailover(raw: unknown, at: At): FailoverPolicy {
  if (raw === undefined || raw === null) return { ...DEFAULT_FAILOVER }
  if (!isRecord(raw)) {
    at("/failover", "Must be an object of failover settings")
    return { ...DEFAULT_FAILOVER }
  }
  const out = { ...DEFAULT_FAILOVER }
  for (const [key, v] of Object.entries(raw)) {
    const here = childPointer("/failover", key)
    switch (key) {
      case "enabled":
      case "wrap":
        if (typeof v === "boolean") out[key] = v
        else at(here, "Must be true or false")
        break
      case "fail_after":
        if (typeof v === "number") out.fail_after = v
        else at(here, "Must be a number")
        break
      case "fail_for":
      case "cycle_pause":
      case "failback_after":
        if (typeof v === "string") out[key] = v
        else at(here, 'Must be a duration, e.g. "5m"')
        break
      default:
        at(here, `Not a failover setting, which are ${FAILOVER_KEYS.join(", ")}`)
    }
  }
  return out
}

/** A JSON connection's "failover" read back (here: its pointer); undefined for none. */
export function readConnectionFailover(raw: unknown, here: string, at: At): ConnectionFailover | undefined {
  if (raw === undefined || raw === null) return undefined
  const ptr = childPointer(here, "failover")
  if (!isRecord(raw)) {
    at(ptr, "Must be an object: auto, fail_after")
    return undefined
  }
  const out: ConnectionFailover = {}
  for (const [key, v] of Object.entries(raw)) {
    const p = childPointer(ptr, key)
    if (!CONNECTION_FAILOVER_KEYS.includes(key)) at(p, `Not a connection failover setting, which are ${CONNECTION_FAILOVER_KEYS.join(", ")}`)
    else if (key === "auto") {
      if (typeof v === "boolean") out.auto = v
      else if (v !== null) at(p, "Must be true or false")
    } else if (typeof v === "number") out.fail_after = v
    else if (v !== null) at(p, "Must be a number")
  }
  return out.auto === undefined && out.fail_after === undefined ? undefined : out
}

/** The policy in a line, for the build's page: "after 5 failures · back to the first after 1h". */
export function failoverSummary(f: FailoverPolicy): string {
  if (!f.enabled) return "Off: only an operator's switch moves the minion"
  const parts = [`moves after ${f.fail_after} failure${f.fail_after === 1 ? "" : "s"} in a row`]
  if (parseGoDuration(f.fail_for)) parts[0] += ` over at least ${f.fail_for}`
  parts.push(f.wrap ? `round the list, ${f.cycle_pause} pause after a failed round` : "stays on the last")
  parts.push(parseGoDuration(f.failback_after) ? `back to the first after ${f.failback_after}` : "never back on its own")
  return parts.join(" · ")
}

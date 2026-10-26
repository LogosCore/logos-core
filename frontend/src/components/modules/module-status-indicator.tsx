import { StateIndicator, type StateHint, type StateTone } from "./state-indicator"

// Registration as core sees it. Registered is green, deregistered a neutral
// "gone on purpose" amber, dead a failure red, blocked an operator's red.
const STATES: Record<string, { tone: StateTone; hint: StateHint }> = {
  registered: {
    tone: "ok",
    hint: {
      title: "Registered",
      body: "Core knows this instance and receives its heartbeats. It accepts the instance's traffic and can manage it.",
    },
  },
  deregistered: {
    tone: "warn",
    hint: {
      title: "Deregistered",
      body: "The instance left — it shut down, or an operator lifted its block. Core rejects its traffic until it registers again.",
    },
  },
  dead: {
    tone: "bad",
    hint: {
      title: "Dead",
      body: "Core stopped receiving heartbeats (3 missed in a row). It rejects the instance's traffic until it registers again.",
    },
  },
  blocked: {
    tone: "bad",
    hint: {
      title: "Blocked",
      body: "An operator blocked this instance. Core refuses its registrations and traffic until it is unblocked; if it is running, it keeps asking.",
    },
  },
}

export function ModuleStatusIndicator({
  status,
  lastHeartbeatAt,
}: {
  status: string
  lastHeartbeatAt?: string | null
}) {
  const s = STATES[status] ?? { tone: "bad" as const, hint: { title: status, body: "Unknown registration state." } }
  const detail = lastHeartbeatAt
    ? `Last heartbeat ${new Date(lastHeartbeatAt).toLocaleString()}`
    : "No heartbeat received yet."
  return (
    <StateIndicator tone={s.tone} hint={{ ...s.hint, detail }}>
      {status}
    </StateIndicator>
  )
}

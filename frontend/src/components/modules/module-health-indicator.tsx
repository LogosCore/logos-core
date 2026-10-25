import { StateIndicator } from "./state-indicator"

// Health is the module's own heartbeat judgement, a platform value defined by
// the lifecycle contract (healthy | degraded), so core can colour it. A module
// that is not registered has no current health: its last report is greyed.
const ABOUT = "The module's own judgement, sent with every heartbeat; core shows it and does not act on it."

export function ModuleHealthIndicator({ health, registered }: { health: string; registered: boolean }) {
  if (!health) {
    return (
      <StateIndicator tone="idle" hint={{ title: "No health yet", body: "The module has not sent a heartbeat yet." }}>
        no health yet
      </StateIndicator>
    )
  }
  if (!registered) {
    return (
      <StateIndicator
        tone="idle"
        hint={{
          title: `Last reported: ${health}`,
          body: "The instance is not registered, so this is out of date.",
          detail: ABOUT,
        }}
      >
        {health}
      </StateIndicator>
    )
  }
  const healthy = health === "healthy"
  return (
    <StateIndicator
      tone={healthy ? "ok" : "warn"}
      hint={{
        title: healthy ? "Healthy" : "Degraded",
        body: healthy
          ? "The module reports that it can do its job."
          : "The module reports it is running but impaired. Check its status values and logs.",
        detail: ABOUT,
      }}
    >
      {health}
    </StateIndicator>
  )
}

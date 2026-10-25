import { StateIndicator } from "./state-indicator"

// Health is the module's own heartbeat judgement, a platform value defined by
// the lifecycle contract (healthy | degraded), so core can colour it. A module
// that is not registered has no current health: its last report is greyed.
export function ModuleHealthIndicator({ health, registered }: { health: string; registered: boolean }) {
  if (!health) {
    return <StateIndicator tone="idle">no health yet</StateIndicator>
  }
  if (!registered) {
    return (
      <StateIndicator tone="idle" title="Last reported before the module went away">
        {health}
      </StateIndicator>
    )
  }
  return (
    <StateIndicator tone={health === "healthy" ? "ok" : "warn"} title="Reported by the module in its heartbeat">
      {health}
    </StateIndicator>
  )
}

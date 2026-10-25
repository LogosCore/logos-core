import { StateIndicator, type StateTone } from "./state-indicator"

// Registered is healthy/green, deregistered a neutral "gone on purpose"
// amber, dead a failure red.
const TONE: Record<string, StateTone> = {
  registered: "ok",
  deregistered: "warn",
  dead: "bad",
}

export function ModuleStatusIndicator({ status, title }: { status: string; title?: string }) {
  return (
    <StateIndicator tone={TONE[status] ?? "bad"} title={title}>
      {status}
    </StateIndicator>
  )
}

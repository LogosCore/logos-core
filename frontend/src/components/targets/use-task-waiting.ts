import { useNow } from "@/hooks/use-now"
import { useMinionTargetChangedSubscription, useSessionReach } from "@/graphql/hooks/minion-targets"
import { formatAgo } from "@/components/minion-sessions/sessions-layout"
import { TARGET_STATE_LABEL } from "./target-values"

/**
 * Why a bind-call session's unfinished tasks are not reaching its minion:
 * it has no target (tasks queued before it was removed wait for one added
 * to the session), or its target's latest call did not reach the minion.
 * null when nothing holds them up: another kind of session, whose minion
 * comes for its tasks, or a connected target, which core calls at once.
 */
export function useTaskWaiting(sessionId: string, { live = false }: { live?: boolean } = {}): string | null {
  const reach = useSessionReach(sessionId).data
  // "since 5m": minutes are fine grained enough for how long a minion is gone.
  const now = useNow(30_000)
  // One subscription keeps it live (the console's task detail): a list's
  // rows read the same query without opening one each.
  useMinionTargetChangedSubscription(live && reach?.bind ? reach.operationId : "")
  if (!reach?.bind) return null
  const t = reach.target
  if (!t) return "no target: core cannot reach the minion until one is added to the session"
  if (t.problems.length > 0) return "the target needs editing: its values no longer fit the channel's fields"
  if (t.state === "connected" || t.state === "new") return null
  if (t.kind === "bind-persistent") {
    if (t.state === "connecting") return "the connection is opening"
    if (t.state === "not_connected") {
      return t.policy === "manual"
        ? "no connection open: Connect the target (its policy is manual)"
        : "no connection open: core opens it now"
    }
  }
  // "for 5m", "for 3h"; past a day formatAgo has no short form, left out.
  const ago = t.unreachableSince ? formatAgo(now - Date.parse(t.unreachableSince)) : null
  const since = ago && ago !== "just now" ? ago.replace(/ ago$/, "") : null
  const label = (TARGET_STATE_LABEL[t.state] ?? t.state).toLowerCase()
  return `the target is ${label}${since ? ` for ${since}` : ""}${t.nextCallAt ? ": core calls it again" : ": Check now calls it again"}`
}

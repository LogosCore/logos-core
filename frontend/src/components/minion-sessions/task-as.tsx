import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { ELEVATED, howLabel, identityUser } from "./identity-values"

/** Who a task ran as, when another than its process (a become): "as root". */
export function TaskAs({ as }: { as: MinionTaskFieldsFragment["as"] }) {
  if (!as?.how) return null
  const who = identityUser(as)
  return (
    <span
      className={`shrink-0 text-xs ${as.integrity && ELEVATED.has(as.integrity) ? "font-medium" : "text-muted-foreground"}`}
      title={`Ran as ${who} (${howLabel(as.how)}${as.integrity ? `, integrity ${as.integrity}` : ""})`}
    >
      as {who}
    </span>
  )
}

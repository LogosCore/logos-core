import { CheckIcon, Loader2Icon, TriangleAlertIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>

export function SyncStateBadge({
  management,
}: {
  management: Pick<Management, "syncState" | "revision" | "appliedRevision" | "lastApplyError">
}) {
  const { syncState, revision, appliedRevision, lastApplyError } = management

  if (syncState === "IN_SYNC") {
    return (
      <Badge
        variant="outline"
        className="border-green-600/40 text-green-700 dark:text-green-400"
        title={`Revision ${revision} applied`}
      >
        <CheckIcon />
        In sync
      </Badge>
    )
  }

  if (syncState === "PENDING") {
    return (
      <Badge
        variant="outline"
        className="border-amber-600/40 text-amber-700 dark:text-amber-400"
        title={`Applied revision ${appliedRevision}`}
      >
        <Loader2Icon className="animate-spin" />
        Applying rev {revision}…
      </Badge>
    )
  }

  const badge = (
    <Badge variant="destructive">
      <TriangleAlertIcon />
      Out of sync
    </Badge>
  )
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" />}>{badge}</TooltipTrigger>
      <TooltipContent className="max-w-sm">
        {lastApplyError ||
          `Revision ${revision} not applied (module is at ${appliedRevision}).`}
      </TooltipContent>
    </Tooltip>
  )
}

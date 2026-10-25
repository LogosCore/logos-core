import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { StateIndicator } from "../state-indicator"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>

/** Whether the module runs the configuration core holds for it. */
export function ModuleSyncIndicator({
  management,
}: {
  management: Pick<Management, "syncState" | "revision" | "appliedRevision" | "lastApplyError">
}) {
  const { syncState, revision, appliedRevision, lastApplyError } = management

  if (syncState === "IN_SYNC") {
    return (
      <StateIndicator tone="ok" title={`Configuration revision ${revision} applied`}>
        in sync
      </StateIndicator>
    )
  }
  if (syncState === "PENDING") {
    return (
      <StateIndicator tone="warn" pulse title={`Applying revision ${revision} (module is at ${appliedRevision})`}>
        syncing
      </StateIndicator>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" />}>
        <StateIndicator tone="bad">out of sync</StateIndicator>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">
        {lastApplyError || `Revision ${revision} not applied (module is at ${appliedRevision}).`}
      </TooltipContent>
    </Tooltip>
  )
}

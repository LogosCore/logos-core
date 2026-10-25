import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { StateIndicator } from "../state-indicator"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>

const ABOUT = "Core holds the module's configuration as numbered revisions; the module reports the revision it applied."

/** Whether the module runs the configuration core holds for it. */
export function ModuleSyncIndicator({
  management,
}: {
  management: Pick<Management, "syncState" | "revision" | "appliedRevision" | "lastApplyError">
}) {
  const { syncState, revision, appliedRevision, lastApplyError } = management

  if (syncState === "IN_SYNC") {
    return (
      <StateIndicator
        tone="ok"
        hint={{
          title: "In sync",
          body: "The module runs the configuration core holds for it.",
          detail: `Revision ${revision} applied. ${ABOUT}`,
        }}
      >
        in sync
      </StateIndicator>
    )
  }
  if (syncState === "PENDING") {
    return (
      <StateIndicator
        tone="warn"
        pulse
        hint={{
          title: "Syncing",
          body: "A newer configuration is being applied.",
          detail: `Revision ${revision} on its way; the module is at ${appliedRevision}.`,
        }}
      >
        syncing
      </StateIndicator>
    )
  }
  return (
    <StateIndicator
      tone="bad"
      hint={{
        title: "Out of sync",
        body: "The module does not run the configuration core holds for it: applying failed, or the instance is not registered.",
        detail: lastApplyError
          ? `Revision ${revision}, module at ${appliedRevision}: ${lastApplyError}`
          : `Revision ${revision}, module at ${appliedRevision}.`,
      }}
    >
      out of sync
    </StateIndicator>
  )
}

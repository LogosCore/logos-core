/** An operator's lifecycle change on a module instance. */
export type ModuleAdminAction = "block" | "unblock" | "delete"

/**
 * The changes an instance in status takes. Any instance but a blocked one can
 * be blocked, which cuts a running one off; only a blocked one unblocked; and
 * only a gone one (deregistered or dead) deleted — a registered one would just
 * register again, and a blocked one's row is its block.
 */
export function adminActions(status: string): ModuleAdminAction[] {
  switch (status) {
    case "blocked":
      return ["unblock"]
    case "deregistered":
    case "dead":
      return ["block", "delete"]
    default:
      return ["block"]
  }
}

// A command a factory says works over some connection kinds only (its build
// schema's kinds, docs: Minion Build, Commands): core refuses its task on a
// session whose minion uses another kind. These say so where commands are
// listed and picked.

/** "reverse-persistent or bind-persistent". */
export function kindsList(kinds: readonly string[]): string {
  if (kinds.length <= 1) return kinds.join("")
  return `${kinds.slice(0, -1).join(", ")} or ${kinds[kinds.length - 1]}`
}

/**
 * What to say of a command's kinds: nothing for one that works over every
 * kind; where it works; and, on a session whose minion uses another kind,
 * that it is switched first (blocked: core would refuse the task).
 */
export function commandKindsNote(
  kinds: readonly string[] | null | undefined,
  connKind?: string | null,
): { text: string; blocked: boolean } | null {
  if (!kinds || kinds.length === 0) return null
  const over = `Over ${kindsList(kinds)} connections only`
  if (connKind && !kinds.includes(connKind)) {
    return { text: `${over}: this minion uses ${connKind}, switch it first`, blocked: true }
  }
  return { text: over, blocked: false }
}

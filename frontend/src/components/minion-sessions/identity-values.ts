// Who a session's tasks run as, or a task ran as (docs:
// contracts/minion-messages/identity), as the web app writes it.

export interface IdentityLike {
  user?: string | null
  domain?: string | null
  integrity?: string | null
  how?: string | null
}

/** The user with its domain, as the hello's user is written: "CORP\admin". */
export function identityUser(i: IdentityLike): string {
  if (!i.user) return ""
  return i.domain && !i.user.includes("\\") ? `${i.domain}\\${i.user}` : i.user
}

/**
 * A user as a cell shows it: "HOST\\name" as "name" when HOST is the session's
 * own machine (a local account: the Host column says where). A domain's
 * prefix stays (CORP\\alice), and so does Windows' NT AUTHORITY: its SYSTEM
 * is not a local account. host may be a full name; its first label counts.
 */
export function shortUser(user: string, host: string | null | undefined): string {
  const i = user.indexOf("\\")
  if (i <= 0 || !host) return user
  const prefix = user.slice(0, i).toLowerCase()
  const machine = host.split(".")[0].toLowerCase()
  return prefix === machine || prefix === host.toLowerCase() ? user.slice(i + 1) : user
}

const HOW: Record<string, string> = {
  impersonation: "impersonating",
  network: "network only",
  become: "became",
}

/** How the work comes to run as the user, for people: "" for the process's own. */
export function howLabel(how: string | null | undefined): string {
  if (!how) return ""
  return HOW[how] ?? how
}

/** "root (became)"; "" for the process's own. */
export function identityLabel(i: IdentityLike | null | undefined): string {
  if (!i || !i.how) return ""
  const who = identityUser(i)
  return `${who} (${howLabel(i.how)})`
}

const SCOPE: Record<string, string> = {
  process: "Process",
  session: "Session",
  task: "Task",
}

/** A history entry's scope, for people. */
export function scopeLabel(scope: string): string {
  return SCOPE[scope] ?? scope
}

// Integrity levels shown as elevated: the usual (user, medium, low) go unsaid.
export const ELEVATED = new Set(["root", "system", "high"])

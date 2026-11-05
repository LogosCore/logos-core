import { credentialTypeLabel } from "@/components/findings/credential-type-utils"
import type { CredentialType, CredentialUsageQuery } from "@/graphql/gql/graphql"

export type CredentialUsage = CredentialUsageQuery["credential"]["usage"]

/** The targets and endpoints naming a credential: live users, whose channel
 * gets it at the next call. */
export function liveUses(u: CredentialUsage | undefined): number {
  return u ? u.targets.length + u.endpoints.length : 0
}

/** "2 targets and 1 endpoint". */
export function usesText(u: CredentialUsage): string {
  const parts = []
  if (u.targets.length > 0) parts.push(`${u.targets.length} target${u.targets.length === 1 ? "" : "s"}`)
  if (u.endpoints.length > 0) parts.push(`${u.endpoints.length} endpoint${u.endpoints.length === 1 ? "" : "s"}`)
  return parts.join(" and ")
}

/**
 * The targets and endpoints that would not take the credential as `type`:
 * a field naming it takes other types. What converting it would break.
 */
export function wouldRefuse(u: CredentialUsage | undefined, type: CredentialType): string[] {
  if (!u) return []
  const refuse = (accept: readonly CredentialType[]) => accept.length > 0 && !accept.includes(type)
  return [
    ...u.targets.filter((t) => refuse(t.accept)).map((t) => `${t.name} (${t.moduleName}, takes ${typesText(t.accept)})`),
    ...u.endpoints.filter((e) => refuse(e.accept)).map((e) => `${e.name} (${e.moduleName}, takes ${typesText(e.accept)})`),
  ]
}

function typesText(types: readonly CredentialType[]): string {
  return types.map((t) => credentialTypeLabel(t)).join(", ")
}

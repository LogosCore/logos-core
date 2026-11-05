import type { CredentialValidity } from "@/graphql/gql/graphql"

// The three states, in the order the filter lists them (the same order as
// CREDENTIAL_VALIDITIES, kept here so this stays a plain module).
const STATES: readonly CredentialValidity[] = ["UNKNOWN", "VALID", "INVALID"]

/** The states a validity filter shows: the store's empty list is all. */
export function shownValidities(
  selected: readonly CredentialValidity[],
): readonly CredentialValidity[] {
  return selected.length > 0 ? selected : STATES
}

/**
 * The filter after a click on one state: shown states hide, hidden ones
 * show. All three shown is stored as the empty list (every state), and so is
 * hiding the last one shown: a list of nothing is a dead end, not a filter.
 */
export function toggleValidity(
  selected: readonly CredentialValidity[],
  v: CredentialValidity,
): CredentialValidity[] {
  const shown = shownValidities(selected)
  const next = shown.includes(v)
    ? shown.filter((x) => x !== v)
    : STATES.filter((x) => x === v || shown.includes(x))
  return next.length === 0 || next.length === STATES.length ? [] : [...next]
}

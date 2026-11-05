import type { CredentialSearchField } from "@/graphql/gql/graphql"

// The selectable credential search fields, in the order they're shown in the
// picker. Mirrors the GraphQL `CredentialSearchField` enum on the backend.
// An empty selection means "search all of these" — the historical default.
// Blobs (keys, certificates, tickets) are not searchable: a term matching
// inside a kilobyte of base64 is noise.
export const CREDENTIAL_SEARCH_FIELDS: CredentialSearchField[] = [
  "NAME",
  "USERNAME",
  "REALM",
  "PASSWORD",
  "HASH",
  "HEX",
  "SERVICE",
  "ALGORITHM",
  "PROPERTIES",
]

const FIELD_LABELS: Record<CredentialSearchField, string> = {
  NAME: "Name",
  USERNAME: "Username",
  REALM: "Realm/Domain",
  PASSWORD: "Password",
  HASH: "Hash",
  HEX: "Hex value",
  SERVICE: "Service",
  ALGORITHM: "Algorithm",
  PROPERTIES: "Properties",
}

export function credentialSearchFieldLabel(f: CredentialSearchField): string {
  return FIELD_LABELS[f]
}

// Human-readable summary of the active field selection, used for the search
// placeholder so users see which fields their query hits. Empty = all
// fields, which is too many to list.
export function describeSearchFields(
  fields: readonly CredentialSearchField[],
): string {
  if (fields.length === 0) return "credentials"
  return fields.map((f) => FIELD_LABELS[f].toLowerCase()).join(", ")
}

import type {
  CredentialField,
  CredentialFieldKind,
} from "@/graphql/gql/graphql"

/**
 * A credential's typed fields, as the GraphQL fragments carry them. Which of
 * them a credential has depends on its type: the server's `credentialTypes`
 * table says, and nothing in the client repeats it.
 */
export interface TypedCredentialFields {
  username: string
  realm: string
  password: string
  key: string
  passphrase: string
  certificate: string
  privateKey: string
  ticket: string
  service: string
  expiresAt?: string | null
  hash: string
  hex: string
  algorithm: string
  keys: readonly { name: string; content: string }[]
}

/** One field of a credential type, as `credentialTypes` lists it. */
export interface CredentialFieldSpecLike {
  field: CredentialField
  label: string
  kind: CredentialFieldKind
  required: boolean
  hint: string
}

/**
 * One field holding a value, in its type's order. The HTML export writes the
 * same shape for the offline viewer (htmlbundle credentialChip).
 */
export interface FilledCredentialField {
  field: CredentialField
  label: string
  kind: CredentialFieldKind
  value: string
}

/** The one-value fields, by the fragment property that holds each. Typed
 * against the schema's enum, so a field added there fails the build here. */
export type CredentialValueField = Exclude<CredentialField, "KEYS">

export const CREDENTIAL_FIELD_PROPERTY: Record<
  CredentialValueField,
  Exclude<keyof TypedCredentialFields, "keys">
> = {
  USERNAME: "username",
  REALM: "realm",
  PASSWORD: "password",
  KEY: "key",
  PASSPHRASE: "passphrase",
  CERTIFICATE: "certificate",
  PRIVATE_KEY: "privateKey",
  TICKET: "ticket",
  SERVICE: "service",
  EXPIRES_AT: "expiresAt",
  HASH: "hash",
  HEX: "hex",
  ALGORITHM: "algorithm",
}

/** A one-value field's value on c, "" when empty. */
export function credentialFieldValue(
  c: TypedCredentialFields,
  field: CredentialValueField,
): string {
  return c[CREDENTIAL_FIELD_PROPERTY[field]] ?? ""
}

/**
 * The fields of a type that hold a value on c, in the type's order. Keys are
 * left out: they are a list (c.keys), shown one by one.
 */
export function filledCredentialFields(
  c: TypedCredentialFields,
  fields: readonly CredentialFieldSpecLike[],
): FilledCredentialField[] {
  const out: FilledCredentialField[] = []
  for (const f of fields) {
    if (f.field === "KEYS") continue
    const value = credentialFieldValue(c, f.field)
    if (value) out.push({ field: f.field, label: f.label, kind: f.kind, value })
  }
  return out
}

function valueOf(
  filled: readonly FilledCredentialField[],
  field: CredentialField,
): string {
  return filled.find((f) => f.field === field)?.value ?? ""
}

/** The account a credential opens: `REALM\user`, or either alone. */
export function credentialAccount(
  filled: readonly FilledCredentialField[],
): string {
  const username = valueOf(filled, "USERNAME")
  const realm = valueOf(filled, "REALM")
  if (realm && username) return `${realm}\\${username}`
  return username || realm
}

/** The one-line secret worth showing beside the account (a password, a
 * hash, a hex key), or "" for a type whose secret is a blob. */
export function credentialSecret(
  filled: readonly FilledCredentialField[],
): string {
  return filled.find((f) => f.kind === "SECRET")?.value ?? ""
}

/** `account:secret`, the segment a chip shows after the name. */
export function credentialSegment(
  filled: readonly FilledCredentialField[],
): string {
  const account = credentialAccount(filled)
  const secret = credentialSecret(filled)
  if (account && secret) return `${account}:${secret}`
  return account || secret
}

/** The many-line values (a key, a certificate, a ticket) and OTHER's named
 * keys, each with its label: what a chip names by hash, not content. */
export function credentialBlobs(
  filled: readonly FilledCredentialField[],
  keys: readonly { name: string; content: string }[],
): { label: string; content: string }[] {
  return [
    ...filled
      .filter((f) => f.kind === "BLOB")
      .map((f) => ({ label: f.label, content: f.value })),
    ...keys.map((k, i) => ({
      label: k.name.trim() || `Key ${i + 1}`,
      content: k.content,
    })),
  ]
}

/** Whether a credential's expiry has passed. */
export function credentialExpired(
  expiresAt: string | null | undefined,
  now: number = Date.now(),
): boolean {
  if (!expiresAt) return false
  const at = Date.parse(expiresAt)
  return Number.isFinite(at) && at <= now
}

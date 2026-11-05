import dayjs from "dayjs"
import type {
  CreateCredentialInput,
  CredentialType,
  CredentialTypeSpec,
  CredentialValidity,
  UpdateCredentialInput,
} from "@/graphql/gql/graphql"
import {
  CREDENTIAL_FIELD_PROPERTY,
  credentialFieldValue,
  type CredentialValueField,
  type TypedCredentialFields,
} from "@/lib/credential-fields"
import {
  keyDraftsFromWire,
  keyDraftsToInputs,
  type KeyDraft,
} from "@/components/findings/credential-key-drafts"
import type { PropertyDraft } from "@/components/findings/credential-property-drafts"

/** The one-value fields' drafts, every field at once: switching the type
 * back and forth keeps what was typed, and only what the type takes is
 * sent. EXPIRES_AT is held as a `datetime-local` value. */
export type CredentialFieldDrafts = Record<CredentialValueField, string>

export interface CredentialFormValues {
  name: string
  type: CredentialType
  fields: CredentialFieldDrafts
  keys: KeyDraft[]
  properties: PropertyDraft[]
  validity: CredentialValidity
  tags: string[]
}

const VALUE_FIELDS = Object.keys(CREDENTIAL_FIELD_PROPERTY) as CredentialValueField[]

export function emptyCredentialFieldDrafts(): CredentialFieldDrafts {
  return Object.fromEntries(VALUE_FIELDS.map((f) => [f, ""])) as CredentialFieldDrafts
}

export function emptyCredentialFormValues(name = ""): CredentialFormValues {
  return {
    name,
    type: "PLAINTEXT",
    fields: emptyCredentialFieldDrafts(),
    keys: [],
    properties: [],
    validity: "UNKNOWN",
    tags: [],
  }
}

/** The `datetime-local` value for an RFC 3339 time, in the viewer's zone. */
export function toDateTimeLocal(iso: string | null | undefined): string {
  if (!iso) return ""
  const d = dayjs(iso)
  return d.isValid() ? d.format("YYYY-MM-DDTHH:mm") : ""
}

/** The RFC 3339 time for a `datetime-local` value; "" stays "". */
export function fromDateTimeLocal(local: string): string {
  if (!local.trim()) return ""
  const d = dayjs(local)
  return d.isValid() ? d.toISOString() : local
}

export function credentialFieldDraftsFrom(
  c: TypedCredentialFields,
): CredentialFieldDrafts {
  const drafts = emptyCredentialFieldDrafts()
  for (const f of VALUE_FIELDS) {
    drafts[f] =
      f === "EXPIRES_AT" ? toDateTimeLocal(c.expiresAt) : credentialFieldValue(c, f)
  }
  return drafts
}

export function credentialKeyDraftsFrom(c: TypedCredentialFields): KeyDraft[] {
  return keyDraftsFromWire(c.keys)
}

function takes(spec: CredentialTypeSpec | undefined, field: string): boolean {
  return !!spec?.fields.some((f) => f.field === field)
}

function wireValue(field: CredentialValueField, draft: string): string {
  return field === "EXPIRES_AT" ? fromDateTimeLocal(draft) : draft
}

type TypedInput = Pick<
  CreateCredentialInput,
  | "username" | "realm" | "password" | "key" | "passphrase" | "certificate"
  | "privateKey" | "ticket" | "service" | "expiresAt" | "hash" | "hex"
  | "algorithm" | "keys"
>

/** What a create sends of the typed fields: those the type takes, and only
 * those with a value. */
export function credentialCreateFields(
  values: CredentialFormValues,
  spec: CredentialTypeSpec | undefined,
): TypedInput {
  const out: TypedInput = {}
  for (const f of VALUE_FIELDS) {
    const v = wireValue(f, values.fields[f])
    if (takes(spec, f) && v) out[CREDENTIAL_FIELD_PROPERTY[f]] = v
  }
  if (takes(spec, "KEYS")) out.keys = keyDraftsToInputs(values.keys)
  return out
}

/**
 * What an update sends of the type and its fields: every field the type
 * takes, as it stands ("" empties one). A change of type also sends empty
 * what the old type held that the new one has no field for, the drops
 * `credentialTypeChangeDrops` listed: the server refuses a change of type
 * that would lose a value it was not told to drop.
 */
export function credentialUpdateFields(
  values: CredentialFormValues,
  spec: CredentialTypeSpec | undefined,
  was: {
    type: CredentialType
    spec: CredentialTypeSpec | undefined
    /** The drafts the form opened with. An expiry left as it was is not
     * sent: its draft is to the minute, the stored time may not be. */
    fields?: CredentialFieldDrafts
  },
): Pick<UpdateCredentialInput, "type"> & TypedInput {
  const out: Pick<UpdateCredentialInput, "type"> & TypedInput = {}
  const changed = values.type !== was.type
  if (changed) out.type = values.type
  for (const f of VALUE_FIELDS) {
    const property = CREDENTIAL_FIELD_PROPERTY[f]
    if (takes(spec, f)) {
      if (f === "EXPIRES_AT" && was.fields && values.fields[f] === was.fields[f]) continue
      out[property] = wireValue(f, values.fields[f])
    } else if (changed && takes(was.spec, f)) out[property] = ""
  }
  if (takes(spec, "KEYS")) out.keys = keyDraftsToInputs(values.keys)
  else if (changed && takes(was.spec, "KEYS")) out.keys = []
  return out
}

/**
 * Seeds the new type's main field from what the old shape held, when a type
 * change leaves it empty: OTHER's first key into a KEY's key, a
 * CERTIFICATE's certificate (the second into its private key) or a TICKET's
 * ticket; a password into a HASH's hash, or a HEX's value when it is hex.
 * A copy, not a move: switching back finds everything where it was, and
 * `credentialTypeChangeDrops` does not count what was carried.
 */
export function carryCredentialFieldsOver(
  values: CredentialFormValues,
  to: CredentialTypeSpec | undefined,
): CredentialFormValues {
  if (!to) return values
  const fields = { ...values.fields }
  const keys = values.keys.map((k) => k.content.trim()).filter(Boolean)
  const fill = (field: CredentialValueField, value: string | undefined) => {
    if (value && takes(to, field) && !fields[field]) fields[field] = value
  }
  if (!takes(to, "KEYS")) {
    if (takes(to, "KEY")) fill("KEY", keys[0])
    if (takes(to, "CERTIFICATE")) {
      fill("CERTIFICATE", keys[0])
      fill("PRIVATE_KEY", keys[1])
    }
    if (takes(to, "TICKET")) fill("TICKET", keys[0])
  }
  if (!takes(to, "PASSWORD")) {
    fill("HASH", fields.PASSWORD)
    if (/^(0x)?[0-9a-fA-F:\s]+$/.test(fields.PASSWORD)) fill("HEX", fields.PASSWORD)
  }
  return { ...values, fields }
}

/**
 * What a change of type would lose: values the old type holds in fields the
 * new one has not, less any the new type's fields now carry. Labels for the
 * warning beside the type, e.g. ["Password", "Key id_rsa"].
 */
export function credentialTypeChangeDrops(
  values: CredentialFormValues,
  spec: CredentialTypeSpec | undefined,
  was: CredentialTypeSpec | undefined,
): string[] {
  if (!spec || !was || spec.type === was.type) return []
  const carried = new Set(
    VALUE_FIELDS.filter((f) => takes(spec, f))
      .map((f) => values.fields[f].trim())
      .filter(Boolean),
  )
  const drops: string[] = []
  for (const f of was.fields) {
    if (takes(spec, f.field)) continue
    if (f.field === "KEYS") {
      values.keys.forEach((k, i) => {
        const content = k.content.trim()
        if ((content || k.name.trim()) && !carried.has(content)) {
          drops.push(`Key ${k.name.trim() || i + 1}`)
        }
      })
      continue
    }
    const v = values.fields[f.field].trim()
    if (v && !carried.has(v)) drops.push(f.label)
  }
  return drops
}

import { describe, expect, it } from "vitest"
import type { CredentialTypeSpec } from "@/graphql/gql/graphql"
import {
  carryCredentialFieldsOver,
  credentialCreateFields,
  credentialTypeChangeDrops,
  credentialUpdateFields,
  emptyCredentialFormValues,
  fromDateTimeLocal,
  toDateTimeLocal,
  type CredentialFormValues,
} from "@/components/findings/credential-form-values"
import { toggleValidity } from "@/components/findings/credential-validity-filter"
import {
  credentialAccount,
  credentialBlobs,
  credentialExpired,
  credentialSegment,
  filledCredentialFields,
  type TypedCredentialFields,
} from "@/lib/credential-fields"

// A slice of the server's type table (models.credentialTypeSpecs).
const field = (
  f: CredentialTypeSpec["fields"][number]["field"],
  label: string,
  kind: CredentialTypeSpec["fields"][number]["kind"],
  required = false,
) => ({ field: f, label, kind, required, hint: "" })
const PLAINTEXT: CredentialTypeSpec = {
  type: "PLAINTEXT", label: "Plaintext", description: "",
  fields: [field("USERNAME", "Username", "TEXT"), field("REALM", "Realm/Domain", "TEXT"), field("PASSWORD", "Password", "SECRET", true)],
}
const KEY: CredentialTypeSpec = {
  type: "KEY", label: "Key", description: "",
  fields: [field("USERNAME", "Username", "TEXT"), field("KEY", "Key", "BLOB", true), field("PASSPHRASE", "Passphrase", "SECRET")],
}
const HASH: CredentialTypeSpec = {
  type: "HASH", label: "Hash", description: "",
  fields: [field("USERNAME", "Username", "TEXT"), field("REALM", "Realm/Domain", "TEXT"), field("HASH", "Hash", "SECRET", true), field("ALGORITHM", "Algorithm", "TEXT")],
}
const TICKET: CredentialTypeSpec = {
  type: "TICKET", label: "Ticket", description: "",
  fields: [field("USERNAME", "Username", "TEXT"), field("TICKET", "Ticket", "BLOB", true), field("EXPIRES_AT", "Expires at", "DATETIME")],
}
const OTHER: CredentialTypeSpec = {
  type: "OTHER", label: "Other", description: "",
  fields: [field("USERNAME", "Username", "TEXT"), field("PASSWORD", "Password", "SECRET"), field("KEYS", "Keys", "KEY_LIST")],
}

function form(
  p: Omit<Partial<CredentialFormValues>, "fields"> & { fields?: Partial<CredentialFormValues["fields"]> },
): CredentialFormValues {
  const base = emptyCredentialFormValues("n")
  return { ...base, ...p, fields: { ...base.fields, ...p.fields } }
}
const key = (name: string, content: string) => ({ _id: name, name, content })

describe("credential form payloads", () => {
  it("creates with only the type's fields that hold a value", () => {
    const values = form({ type: "PLAINTEXT", fields: { USERNAME: "admin", PASSWORD: "pw", KEY: "stray" } })
    expect(credentialCreateFields(values, PLAINTEXT)).toEqual({ username: "admin", password: "pw" })
  })

  it("sends OTHER's keys, and an expiry as UTC", () => {
    expect(credentialCreateFields(form({ type: "OTHER", keys: [key("id_rsa", "K")] }), OTHER)).toEqual({
      keys: [{ name: "id_rsa", content: "K" }],
    })
    const expiry = credentialCreateFields(form({ type: "TICKET", fields: { TICKET: "doIF", EXPIRES_AT: "2026-11-05T18:00" } }), TICKET)
    expect(expiry.expiresAt).toBe(new Date("2026-11-05T18:00").toISOString())
  })

  it("updates every field of the type, and no type when it stays", () => {
    const values = form({ type: "PLAINTEXT", fields: { USERNAME: "admin", PASSWORD: "pw" } })
    expect(credentialUpdateFields(values, PLAINTEXT, { type: "PLAINTEXT", spec: PLAINTEXT })).toEqual({
      username: "admin", realm: "", password: "pw",
    })
  })

  it("leaves an expiry nobody edited alone", () => {
    const drafts = form({ type: "TICKET", fields: { TICKET: "t", EXPIRES_AT: "2026-11-05T18:00" } })
    const was = { type: "TICKET" as const, spec: TICKET, fields: drafts.fields }
    expect(credentialUpdateFields(drafts, TICKET, was)).not.toHaveProperty("expiresAt")
    const edited = { ...drafts, fields: { ...drafts.fields, EXPIRES_AT: "" } }
    expect(credentialUpdateFields(edited, TICKET, was).expiresAt).toBe("")
  })

  it("a change of type empties what the old type held and the new has no room for", () => {
    const values = form({ type: "KEY", fields: { USERNAME: "root", PASSWORD: "pw", KEY: "K" }, keys: [key("id_rsa", "K")] })
    expect(credentialUpdateFields(values, KEY, { type: "OTHER", spec: OTHER })).toEqual({
      type: "KEY", username: "root", key: "K", passphrase: "", password: "", keys: [],
    })
  })
})

describe("converting a credential", () => {
  it("carries OTHER's first key into a KEY, and does not count it as dropped", () => {
    const other = form({ type: "OTHER", fields: { USERNAME: "root" }, keys: [key("id_rsa", "K1"), key("id_ed", "K2")] })
    const converted = carryCredentialFieldsOver({ ...other, type: "KEY" }, KEY)
    expect(converted.fields.KEY).toBe("K1")
    expect(converted.keys).toHaveLength(2) // a copy: switching back finds them
    expect(credentialTypeChangeDrops(converted, KEY, OTHER)).toEqual(["Key id_ed"])
  })

  it("carries a password into a HASH's hash", () => {
    const other = form({ type: "OTHER", fields: { PASSWORD: "aad3b435" } })
    const converted = carryCredentialFieldsOver({ ...other, type: "HASH" }, HASH)
    expect(converted.fields.HASH).toBe("aad3b435")
    expect(credentialTypeChangeDrops(converted, HASH, OTHER)).toEqual([])
  })

  it("names what a change would lose", () => {
    const values = form({ type: "KEY", fields: { PASSWORD: "pw", KEY: "K" } })
    expect(credentialTypeChangeDrops(values, KEY, PLAINTEXT)).toEqual(["Password"])
    expect(credentialTypeChangeDrops(values, PLAINTEXT, PLAINTEXT)).toEqual([])
  })

  it("does not overwrite a field already filled", () => {
    const values = form({ type: "KEY", fields: { KEY: "mine" }, keys: [key("id_rsa", "K1")] })
    expect(carryCredentialFieldsOver(values, KEY).fields.KEY).toBe("mine")
  })
})

describe("datetime-local values", () => {
  it("round-trip through RFC 3339", () => {
    const iso = fromDateTimeLocal("2026-11-05T18:00")
    expect(toDateTimeLocal(iso)).toBe("2026-11-05T18:00")
    expect(fromDateTimeLocal("")).toBe("")
    expect(toDateTimeLocal(null)).toBe("")
  })
})

describe("credential field helpers", () => {
  const cred: TypedCredentialFields = {
    username: "svc", realm: "CORP", password: "", key: "", passphrase: "", certificate: "",
    privateKey: "", ticket: "doIF", service: "cifs/dc01", expiresAt: "2026-11-05T16:00:00Z",
    hash: "", hex: "", algorithm: "", keys: [],
  }
  it("lists the type's filled fields in its order", () => {
    const filled = filledCredentialFields(cred, TICKET.fields)
    expect(filled.map((f) => f.field)).toEqual(["USERNAME", "TICKET", "EXPIRES_AT"])
    expect(credentialAccount(filledCredentialFields(cred, HASH.fields))).toBe("CORP\\svc")
    expect(credentialBlobs(filled, [{ name: "", content: "x" }])).toEqual([
      { label: "Ticket", content: "doIF" },
      { label: "Key 1", content: "x" },
    ])
  })

  it("segments as account:secret", () => {
    const plain = { ...cred, password: "pw" }
    expect(credentialSegment(filledCredentialFields(plain, PLAINTEXT.fields))).toBe("CORP\\svc:pw")
    expect(credentialSegment(filledCredentialFields(cred, KEY.fields))).toBe("svc")
  })

  it("knows an expired ticket", () => {
    const at = Date.parse("2026-11-05T16:00:00Z")
    expect(credentialExpired("2026-11-05T16:00:00Z", at + 1)).toBe(true)
    expect(credentialExpired("2026-11-05T16:00:00Z", at - 1)).toBe(false)
    expect(credentialExpired(null)).toBe(false)
  })
})

describe("the validity filter", () => {
  it("hides a shown state and shows a hidden one", () => {
    // The default: untested and valid shown, invalid hidden.
    expect(toggleValidity(["UNKNOWN", "VALID"], "INVALID")).toEqual([])
    expect(toggleValidity(["UNKNOWN", "VALID"], "VALID")).toEqual(["UNKNOWN"])
    // Every state (the empty list): a click hides that one.
    expect(toggleValidity([], "INVALID")).toEqual(["UNKNOWN", "VALID"])
    // Showing a state keeps the list's order.
    expect(toggleValidity(["INVALID"], "UNKNOWN")).toEqual(["UNKNOWN", "INVALID"])
  })

  it("never filters down to nothing", () => {
    expect(toggleValidity(["VALID"], "VALID")).toEqual([])
  })
})

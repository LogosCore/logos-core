import { describe, expect, it } from "vitest"
import { checkDraft, initialDraft, type FieldDef } from "./field-values"
import { draftFromText } from "@/components/command-form/command-drafts"
import { liveUses, usesText, wouldRefuse, type CredentialUsage } from "@/components/findings/credential-usage-text"

const one = { key: "login", type: "credential", label: "Login", required: true, accept: ["KEY"] } as unknown as FieldDef
const list = { key: "logins", type: "credential_list", label: "Logins", required: false, maxItems: 2 } as unknown as FieldDef

describe("a credential field", () => {
  it("holds a credential's id", () => {
    const d = initialDraft(one, "c1")
    expect(d).toEqual({ kind: "credentials", ids: ["c1"], multiple: false })
    expect(checkDraft(one, d)).toEqual({ ok: true, value: "c1" })
    expect(checkDraft(one, { kind: "credentials", ids: [], multiple: false }).ok).toBe(false) // required
  })
  it("a list holds ids in the order they are tried", () => {
    const d = initialDraft(list, ["c2", "c1"])
    expect(d).toEqual({ kind: "credentials", ids: ["c2", "c1"], multiple: true })
    expect(checkDraft(list, d)).toEqual({ ok: true, value: ["c2", "c1"] })
    expect(checkDraft(list, { kind: "credentials", ids: [], multiple: true })).toEqual({ ok: true, value: null })
    expect(checkDraft(list, { kind: "credentials", ids: ["a", "b", "c"], multiple: true }).ok).toBe(false)
  })
  it("typed on a line, is ids", () => {
    expect(draftFromText(list, "c1, c2")).toEqual({ kind: "credentials", ids: ["c1", "c2"], multiple: true })
    expect(draftFromText(one, "c1")).toEqual({ kind: "credentials", ids: ["c1"], multiple: false })
  })
})

const usage = {
  __typename: "CredentialUsage",
  targets: [
    { id: "t1", name: "10.0.0.7", moduleName: "ssh", kind: "bind-call", buildId: "b1", accept: ["KEY", "PLAINTEXT"], login: null },
    { id: "t2", name: "10.0.0.8", moduleName: "ssh", kind: "bind-call", buildId: "b1", accept: [], login: null },
  ],
  endpoints: [{ id: "e1", name: "edge", moduleName: "http", kind: "reverse-call", accept: ["PLAINTEXT"] }],
  builds: 1,
  tasks: 0,
} as unknown as CredentialUsage

describe("a credential's usage", () => {
  it("counts the targets and endpoints that name it", () => {
    expect(liveUses(usage)).toBe(3)
    expect(usesText(usage)).toBe("2 targets and 1 endpoint")
  })
  it("names what would not take it as another type", () => {
    expect(wouldRefuse(usage, "KEY")).toEqual(["edge (http, takes Plaintext)"])
    expect(wouldRefuse(usage, "HEX")).toEqual(["10.0.0.7 (ssh, takes Key, Plaintext)", "edge (http, takes Plaintext)"])
    expect(wouldRefuse(usage, "PLAINTEXT")).toEqual([])
    expect(wouldRefuse(undefined, "HEX")).toEqual([])
  })
})

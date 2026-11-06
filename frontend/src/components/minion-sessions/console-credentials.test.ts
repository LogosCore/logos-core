import { describe, expect, it } from "vitest"
import { credentialNames, matchCredential } from "./console-credentials"

describe("console credentials", () => {
  const c = [
    { id: "1", name: "root pw" },
    { id: "2", name: "Root PW" },
    { id: "3", name: "db" },
  ]
  it("takes the exact name, else the only one of any case", () => {
    expect(matchCredential("root pw", c)).toEqual({ ok: true, id: "1" })
    expect(matchCredential("DB", c)).toEqual({ ok: true, id: "3" })
    expect(matchCredential("ROOT pw", c).ok).toBe(false)
    expect(matchCredential("none", c).ok).toBe(false)
  })
  it("reads a list's names", () => {
    expect(credentialNames("a, b,,c")).toEqual(["a", "b", "c"])
    expect(credentialNames(["a", ""])).toEqual(["a"])
  })
})

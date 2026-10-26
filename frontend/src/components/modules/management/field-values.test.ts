import { describe, expect, it } from "vitest"
import {
  checkDraft,
  durationMs,
  evaluateForm,
  evaluateParams,
  formatElapsed,
  formatValue,
  groupByField,
  humanizeBytes,
  initialDraft,
  initialDrafts,
  initialParamDrafts,
  initialValue,
  localInputToRFC3339,
  mergeActivity,
  parseDraft,
  parseGoDuration,
  resetDraft,
  rfc3339ToLocalInput,
  splitBytes,
  validateValue,
  type Draft,
  type FieldDef,
  type PropertyLike,
} from "./field-values"

function field(p: Partial<FieldDef> & { key: string; type: string }): FieldDef {
  return {
    label: p.key,
    order: 0,
    required: false,
    ...p,
  } as FieldDef
}

function prop(
  f: FieldDef,
  p: Partial<Omit<PropertyLike, "field">> = {},
): PropertyLike {
  return { field: f, source: "default", isSet: false, effective: null, value: null, ...p }
}

describe("bytes", () => {
  it("splits into the largest exact unit", () => {
    expect(splitBytes(0)).toEqual({ text: "0", unit: "B" })
    expect(splitBytes(1000)).toEqual({ text: "1000", unit: "B" })
    expect(splitBytes(1024)).toEqual({ text: "1", unit: "KiB" })
    expect(splitBytes(1536)).toEqual({ text: "1536", unit: "B" })
    expect(splitBytes(3 * 1024 * 1024)).toEqual({ text: "3", unit: "MiB" })
  })

  it("humanizes with binary units", () => {
    expect(humanizeBytes(0)).toBe("0 B")
    expect(humanizeBytes(512)).toBe("512 B")
    expect(humanizeBytes(1536)).toBe("1.5 KiB")
    expect(humanizeBytes(10 * 1024 * 1024)).toBe("10 MiB")
    expect(humanizeBytes(3 * 1024 ** 3)).toBe("3 GiB")
  })

  it("parses amount × unit back to integer bytes", () => {
    const f = field({ key: "buf", type: "bytes" })
    expect(parseDraft(f, { kind: "bytes", text: "4", unit: "MiB" })).toEqual({ ok: true, value: 4 * 1024 * 1024 })
    expect(parseDraft(f, { kind: "bytes", text: "1.5", unit: "KiB" })).toEqual({ ok: true, value: 1536 })
    expect(parseDraft(f, { kind: "bytes", text: "1.3", unit: "B" }).ok).toBe(false)
    expect(parseDraft(f, { kind: "bytes", text: "abc", unit: "B" }).ok).toBe(false)
    expect(parseDraft(f, { kind: "bytes", text: "", unit: "KiB" })).toEqual({ ok: true, value: null })
  })

  it("round-trips through a draft", () => {
    const f = field({ key: "buf", type: "bytes" })
    const d = initialDraft(f, 8 * 1024 * 1024)
    expect(d).toEqual({ kind: "bytes", text: "8", unit: "MiB" })
    expect(parseDraft(f, d)).toEqual({ ok: true, value: 8 * 1024 * 1024 })
  })
})

describe("durations", () => {
  it("parses Go duration syntax", () => {
    expect(parseGoDuration("30s")).toBe(30e9)
    expect(parseGoDuration("1m30s")).toBe(90e9)
    expect(parseGoDuration("1.5h")).toBe(5400e9)
    expect(parseGoDuration("500ms")).toBe(500e6)
    expect(parseGoDuration("0")).toBe(0)
    expect(parseGoDuration("-2s")).toBe(-2e9)
  })

  it("rejects what Go rejects", () => {
    expect(parseGoDuration("30")).toBeNull()
    expect(parseGoDuration("1d")).toBeNull()
    expect(parseGoDuration("s")).toBeNull()
    expect(parseGoDuration("1m 30s")).toBeNull()
    expect(parseGoDuration("")).toBeNull()
  })

  it("keeps the string as entered and validates bounds against parsed time", () => {
    const f = field({ key: "t", type: "duration", min: "1s", max: "1m" })
    expect(checkDraft(f, { kind: "text", text: "30s" })).toEqual({ ok: true, value: "30s" })
    expect(checkDraft(f, { kind: "text", text: "500ms" })).toEqual({ ok: false, error: "Must be at least 1s" })
    expect(checkDraft(f, { kind: "text", text: "2m" })).toEqual({ ok: false, error: "Must be at most 1m" })
    expect(checkDraft(f, { kind: "text", text: "soon" }).ok).toBe(false)
  })
})

describe("string_list", () => {
  const f = field({ key: "hosts", type: "string_list" })

  it("is one item per line, blank lines dropped", () => {
    expect(parseDraft(f, { kind: "text", text: " a \n\nb\n" })).toEqual({ ok: true, value: ["a", "b"] })
    expect(parseDraft(f, { kind: "text", text: "\n \n" })).toEqual({ ok: true, value: null })
  })

  it("renders a stored list one per line", () => {
    expect(initialDraft(f, ["x", "y"])).toEqual({ kind: "text", text: "x\ny" })
  })

  it("applies pattern and maxLength per item", () => {
    const g = field({ key: "h", type: "string_list", pattern: "^[a-z]+$", maxLength: 3 })
    expect(validateValue(g, ["abc"])).toBeNull()
    expect(validateValue(g, ["abcd"])).toMatch(/at most 3/)
    expect(validateValue(g, ["AB"])).toMatch(/must match/)
  })
})

describe("numbers", () => {
  it("int requires integer text", () => {
    const f = field({ key: "n", type: "int", min: 1, max: 10 })
    expect(checkDraft(f, { kind: "text", text: "5" })).toEqual({ ok: true, value: 5 })
    expect(checkDraft(f, { kind: "text", text: "5.5" }).ok).toBe(false)
    expect(checkDraft(f, { kind: "text", text: "0" })).toEqual({ ok: false, error: "Must be at least 1" })
    expect(checkDraft(f, { kind: "text", text: "11" })).toEqual({ ok: false, error: "Must be at most 10" })
  })

  it("number accepts decimals", () => {
    const f = field({ key: "r", type: "number", max: 1 })
    expect(checkDraft(f, { kind: "text", text: "0.25" })).toEqual({ ok: true, value: 0.25 })
    expect(checkDraft(f, { kind: "text", text: "1.5" }).ok).toBe(false)
    expect(checkDraft(f, { kind: "text", text: "x" }).ok).toBe(false)
  })

  it("bytes bounds are formatted humanized", () => {
    const f = field({ key: "b", type: "bytes", max: 1024 })
    expect(validateValue(f, 2048)).toBe("Must be at most 1 KiB")
  })
})

describe("validateValue", () => {
  it("required only fails without a fallback", () => {
    const f = field({ key: "s", type: "string", required: true })
    expect(validateValue(f, null)).toBe("Required")
    expect(validateValue(f, "")).toBe("Required")
    expect(validateValue(f, null, { fallback: "x" })).toBeNull()
  })

  it("checks pattern (unanchored, like Go) and maxLength", () => {
    const f = field({ key: "s", type: "string", pattern: "^v\\d+$", maxLength: 4 })
    expect(validateValue(f, "v12")).toBeNull()
    expect(validateValue(f, "x12")).toMatch(/must match/i)
    expect(validateValue(f, "v1234")).toMatch(/at most 4/i)
    const g = field({ key: "s", type: "string", pattern: "\\d" })
    expect(validateValue(g, "abc1")).toBeNull()
  })

  it("checks enum options", () => {
    const f = field({ key: "e", type: "enum", options: [{ value: "a" }, { value: "b", label: "B" }] })
    expect(validateValue(f, "a")).toBeNull()
    expect(validateValue(f, "c")).toMatch(/allowed/)
  })

  it("checks urls", () => {
    const f = field({ key: "u", type: "url" })
    expect(validateValue(f, "https://example.com/x")).toBeNull()
    expect(validateValue(f, "example.com")).toMatch(/URL/)
  })
})

describe("formatValue", () => {
  it("formats per type", () => {
    expect(formatValue("string", null)).toBe("—")
    expect(formatValue("bytes", 2048)).toBe("2 KiB")
    expect(formatValue("duration", "30s", "s")).toBe("30s")
    expect(formatValue("int", 5, "req/s")).toBe("5 req/s")
    expect(formatValue("bool", true)).toBe("true")
    expect(formatValue("string_list", ["a", "b"])).toBe("a, b")
    expect(formatValue("secret", "hunter2")).toBe("•••")
    expect(formatValue("string", { a: 1 })).toBe('{"a":1}')
  })
})

describe("groupByField", () => {
  it("groups, orders by order then label, ungrouped as General", () => {
    const fields = [
      field({ key: "c", type: "string", label: "C", group: "Network", order: 2 }),
      field({ key: "a", type: "string", label: "A", order: 1 }),
      field({ key: "b", type: "string", label: "B", group: "Network", order: 0 }),
      field({ key: "d", type: "string", label: "Alpha", group: "Network", order: 2 }),
    ]
    const groups = groupByField(fields, (f) => f)
    expect(groups.map((g) => g.group)).toEqual(["Network", "General"])
    expect(groups[0].items.map((f) => f.key)).toEqual(["b", "d", "c"])
    expect(groups[1].items.map((f) => f.key)).toEqual(["a"])
  })
})

describe("evaluateForm", () => {
  const port = field({ key: "port", type: "int", label: "Port", min: 1, max: 65535, default: 80 })
  const verbose = field({ key: "verbose", type: "bool", label: "Verbose", default: false })
  const token = field({ key: "token", type: "secret", label: "Token" })
  const name = field({ key: "name", type: "string", label: "Name", required: true })
  const level = field({ key: "level", type: "enum", label: "Level", default: "info", options: [{ value: "info" }, { value: "debug" }] })

  const props = [
    prop(port, { value: 8080, effective: 8080, source: "set", isSet: true }),
    prop(verbose, { effective: false }),
    prop(token, { isSet: true, source: "set" }),
    prop(name, { effective: "x", value: "x", source: "set", isSet: true }),
    prop(level, { effective: "info" }),
  ]

  it("pre-fills controls with the effective value, never a secret", () => {
    expect(props.map(initialValue)).toEqual([8080, false, null, "x", "info"])
    const drafts = initialDrafts(props)
    expect(drafts.level).toEqual({ kind: "text", text: "info" })
    expect(drafts.verbose).toEqual({ kind: "bool", value: false })
    expect(drafts.token).toEqual({ kind: "secret", mode: "keep", text: "" })
  })

  it("has no diff when nothing changed", () => {
    expect(evaluateForm(props, initialDrafts(props))).toEqual({ diff: [], errors: {} })
  })

  it("lists changes with old → new, secrets masked, defaults marked", () => {
    const drafts: Record<string, Draft> = {
      ...initialDrafts(props),
      port: { kind: "text", text: "9090" },
      verbose: { kind: "bool", value: true },
      token: { kind: "secret", mode: "replace", text: "s3cret" },
      level: { kind: "text", text: "debug" },
    }
    const { diff, errors } = evaluateForm(props, drafts)
    expect(errors).toEqual({})
    expect(diff.map((d) => [d.key, d.oldText, d.newText])).toEqual([
      ["level", "info (default)", "debug"],
      ["port", "8080", "9090"],
      ["token", "•••", "•••"],
      ["verbose", "false (default)", "true"],
    ])
    expect(diff.find((d) => d.key === "token")?.new).toBe("s3cret")
    expect(diff.find((d) => d.key === "level")).toMatchObject({ old: null, new: "debug" })
  })

  it("reset to default sends null for a stored value", () => {
    const drafts = { ...initialDrafts(props), port: resetDraft(port) }
    const { diff } = evaluateForm(props, drafts)
    expect(diff).toHaveLength(1)
    expect(diff[0]).toMatchObject({ key: "port", old: 8080, new: null, oldText: "8080", newText: "80 (default)" })
  })

  it("emptying a property that is already the default is no change", () => {
    const drafts = { ...initialDrafts(props), level: resetDraft(level), verbose: resetDraft(verbose) }
    expect(evaluateForm(props, drafts).diff).toEqual([])
  })

  it("clearing a secret only diffs if one is stored", () => {
    const unset = props.map((p) => (p.field.key === "token" ? { ...p, isSet: false, source: "default" } : p))
    const drafts = { ...initialDrafts(unset), token: resetDraft(token) }
    expect(evaluateForm(unset, drafts).diff).toEqual([])
    const set = { ...initialDrafts(props), token: resetDraft(token) }
    expect(evaluateForm(props, set).diff[0]).toMatchObject({ key: "token", new: null, newText: "Not set" })
  })

  it("reports errors and excludes invalid keys from the diff", () => {
    const drafts = {
      ...initialDrafts(props),
      port: { kind: "text", text: "70000" } as Draft,
      name: { kind: "text", text: "" } as Draft,
    }
    const { diff, errors } = evaluateForm(props, drafts)
    expect(diff).toEqual([])
    expect(errors).toEqual({ port: "Must be at most 65535", name: "Required" })
  })

  it("required is satisfied by the module default when reset", () => {
    const withDefault = field({ key: "host", type: "string", label: "Host", required: true, default: "localhost" })
    const ps = [prop(withDefault, { value: "h", effective: "h", source: "set", isSet: true })]
    const { diff, errors } = evaluateForm(ps, { host: resetDraft(withDefault) })
    expect(errors).toEqual({})
    expect(diff[0]).toMatchObject({ key: "host", old: "h", new: null })
  })
})

describe("action params", () => {
  it("starts from defaults and omits unset params", () => {
    const params = [
      field({ key: "count", type: "int", default: 3 }),
      field({ key: "reason", type: "string" }),
      field({ key: "force", type: "bool" }),
    ]
    const drafts = initialParamDrafts(params)
    expect(evaluateParams(params, drafts)).toEqual({ values: { count: 3 }, errors: {} })
  })

  it("validates required params and edits secrets as text", () => {
    const params = [field({ key: "pw", type: "secret", required: true })]
    const drafts = initialParamDrafts(params)
    expect(drafts.pw).toEqual({ kind: "text", text: "" })
    expect(evaluateParams(params, drafts).errors).toEqual({ pw: "Required" })
    expect(evaluateParams(params, { pw: { kind: "text", text: "x" } }).values).toEqual({ pw: "x" })
  })
})

describe("activity", () => {
  it("merges newest first", () => {
    const actor = { type: "user", id: "u1", name: "Ann" }
    const items = mergeActivity(
      [
        { id: "c1", instance: "h1", revision: 1, changes: [], actor, at: "2026-01-01T10:00:00Z" },
        { id: "c2", instance: "h1", revision: 2, changes: [], actor, at: "2026-01-01T12:00:00Z" },
      ],
      [
        { id: "i1", instance: "h1", action: "flush", status: "SUCCEEDED", actor, startedAt: "2026-01-01T11:00:00Z" },
      ],
    )
    expect(items.map((i) => (i.kind === "config" ? i.change.id : i.invocation.id))).toEqual(["c2", "i1", "c1"])
  })

  it("computes and formats durations", () => {
    expect(durationMs("2026-01-01T00:00:00Z", null)).toBeNull()
    expect(durationMs("2026-01-01T00:00:00Z", "2026-01-01T00:00:01.5Z")).toBe(1500)
    expect(formatElapsed(250)).toBe("250ms")
    expect(formatElapsed(1500)).toBe("1.5s")
    expect(formatElapsed(90_000)).toBe("1m30s")
  })
})

describe("string_map", () => {
  const headers = {
    key: "headers",
    type: "string_map",
    label: "Headers",
    order: 0,
    required: false,
    keyPattern: "^[A-Za-z-]+$",
    pattern: "^[^\\r\\n]*$",
    maxItems: 2,
  } as FieldDef

  it("drafts rows from an object and parses them back", () => {
    const draft = initialDraft(headers, { "X-A": "1", "X-B": "2" })
    expect(draft).toEqual({ kind: "map", entries: [{ key: "X-A", value: "1" }, { key: "X-B", value: "2" }] })
    expect(parseDraft(headers, draft)).toEqual({ ok: true, value: { "X-A": "1", "X-B": "2" } })
  })

  it("ignores blank rows and reads no rows as unset", () => {
    expect(parseDraft(headers, { kind: "map", entries: [{ key: "", value: "" }] })).toEqual({ ok: true, value: null })
    expect(initialDraft(headers, null)).toEqual({ kind: "map", entries: [] })
  })

  it("rejects a missing key and duplicates", () => {
    expect(parseDraft(headers, { kind: "map", entries: [{ key: "", value: "x" }] }).ok).toBe(false)
    const dup = parseDraft(headers, {
      kind: "map",
      entries: [
        { key: "X-A", value: "1" },
        { key: " X-A ", value: "2" },
      ],
    })
    expect(dup.ok).toBe(false)
  })

  it("validates keys, values and the entry count", () => {
    expect(validateValue(headers, { "X A": "1" })).toMatch(/not a valid key/)
    expect(validateValue(headers, { "X-A": "a\nb" })).toMatch(/must match/)
    expect(validateValue(headers, { A: "1", B: "2", C: "3" })).toMatch(/At most 2/)
    expect(validateValue(headers, { "X-A": "1" })).toBeNull()
  })

  it("formats as key: value pairs", () => {
    expect(formatValue("string_map", { "X-A": "1", "X-B": "2" })).toBe("X-A: 1, X-B: 2")
  })
})

describe("datetime, enum_list and file fields", () => {
  const datetime = { key: "kill_date", type: "datetime", label: "Kill date", order: 0, required: false,
    min: "2026-01-01T00:00:00Z", max: "2030-01-01T00:00:00Z" } as FieldDef
  const options = { key: "punches", type: "enum_list", label: "Punches", order: 0, required: true, maxItems: 2,
    options: [{ value: "jab" }, { value: "cross" }, { value: "hook" }] } as FieldDef
  const file = { key: "badge", type: "file", label: "Badge", order: 0, required: false } as FieldDef

  it("edits a datetime in local time and sends it in UTC", () => {
    const draft = initialDraft(datetime, "2027-01-31T00:00:00Z")
    expect(draft.kind).toBe("text")
    // Whatever the local zone, the round trip is the same instant.
    expect(checkDraft(datetime, draft)).toEqual({ ok: true, value: "2027-01-31T00:00:00Z" })
    expect(localInputToRFC3339(rfc3339ToLocalInput("2027-06-15T12:30:45Z"))).toBe("2027-06-15T12:30:45Z")
    expect(checkDraft(datetime, { kind: "text", text: "31.01.2027" })).toEqual({ ok: false, error: "Not a date and time" })
    expect(checkDraft(datetime, { kind: "text", text: "" })).toEqual({ ok: true, value: null })
    expect(validateValue(datetime, "2025-12-31T23:59:59Z")).toMatch(/at least/i)
    expect(validateValue(datetime, "2031-01-01T00:00:00Z")).toMatch(/at most/i)
    expect(validateValue(datetime, "2027-01-31")).toBe("Must be a date and time")
  })

  it("keeps enum_list picks in the declared order", () => {
    expect(initialDraft(options, ["hook", "jab"])).toEqual({ kind: "set", values: ["hook", "jab"] })
    expect(checkDraft(options, { kind: "set", values: ["hook", "jab"] })).toEqual({ ok: true, value: ["jab", "hook"] })
    expect(checkDraft(options, { kind: "set", values: [] })).toEqual({ ok: false, error: "Required" })
    expect(validateValue(options, ["jab", "cross", "hook"])).toBe("At most 2")
    expect(validateValue(options, ["uppercut"])).toMatch(/not one of/)
    expect(validateValue(options, ["jab", "jab"])).toMatch(/twice/)
    expect(formatValue("enum_list", ["jab", "hook"])).toBe("jab, hook")
  })

  it("holds a file by its id", () => {
    expect(initialDraft(file, "f-1")).toEqual({ kind: "file", id: "f-1" })
    expect(initialDraft(file, null)).toEqual({ kind: "file", id: null })
    expect(checkDraft(file, { kind: "file", id: "f-1" })).toEqual({ ok: true, value: "f-1" })
    expect(checkDraft(file, { kind: "file", id: null })).toEqual({ ok: true, value: null })
    expect(checkDraft({ ...file, required: true }, { kind: "file", id: null })).toEqual({ ok: false, error: "Required" })
  })
})

import { describe, expect, it } from "vitest"
import { factoryHeld, factoryStands, ranOn, type FactoryRun } from "./factory-values"

const run = (f: Partial<FactoryRun> = {}): FactoryRun => ({
  module: "example",
  instance: null,
  acceptedAt: null,
  reportAt: null,
  waiting: null,
  waitingSince: null,
  resultAt: null,
  ...f,
})

describe("factoryStands", () => {
  it("waits for an instance, with core's reason once it gave one", () => {
    expect(factoryStands({ state: "FACTORY", factory: run() })).toEqual({ phase: "waiting", why: null, module: "example" })
    expect(factoryStands({ state: "FACTORY", factory: run({ waiting: "example-1 is busy", waitingSince: "2026-11-22T10:00:00Z" }) })).toEqual({
      phase: "waiting",
      why: "example-1 is busy",
      module: "example",
    })
    expect(factoryStands({ state: "FACTORY", factory: run({ waiting: "  " }) })).toEqual({ phase: "waiting", why: null, module: "example" })
  })
  it("is offered to an instance, then runs there", () => {
    expect(factoryStands({ state: "FACTORY", factory: run({ instance: "example-1" }) })).toEqual({ phase: "offered", instance: "example-1" })
    expect(factoryStands({ state: "FACTORY", factory: run({ instance: "example-1", acceptedAt: "2026-11-22T10:00:01Z" }) })).toEqual({
      phase: "running",
      instance: "example-1",
    })
  })
  it("says nothing once it ended, or of another task", () => {
    expect(factoryStands({ state: "DONE", factory: run({ instance: "example-1", acceptedAt: "2026-11-22T10:00:01Z" }) })).toBeNull()
    expect(factoryStands({ state: "QUEUED", factory: null })).toBeNull()
  })
})

describe("factoryHeld", () => {
  it("is core's reason while no instance took it", () => {
    expect(factoryHeld({ state: "FACTORY", factory: run({ waiting: 'no registered "example" factory instance runs whois' }) })).toBe(
      'no registered "example" factory instance runs whois',
    )
    expect(factoryHeld({ state: "FACTORY", factory: run() })).toBeNull()
  })
  it("is null once an instance has it, it ended, or for another task", () => {
    expect(factoryHeld({ state: "FACTORY", factory: run({ instance: "example-1", waiting: "example-2 is busy" }) })).toBeNull()
    expect(factoryHeld({ state: "CANCELLED", factory: run({ waiting: "example-1 is busy" }) })).toBeNull()
    expect(factoryHeld({ state: "QUEUED", factory: null })).toBeNull()
  })
})

describe("ranOn", () => {
  it("is the instance that took it, once it ended", () => {
    expect(ranOn({ state: "DONE", factory: run({ instance: "example-1", acceptedAt: "2026-11-22T10:00:01Z" }) })).toBe("example-1")
    expect(ranOn({ state: "FAILED", factory: run({ instance: "example-1", acceptedAt: "2026-11-22T10:00:01Z" }) })).toBe("example-1")
  })
  it("is null while it runs, for one no instance took, and for another task", () => {
    expect(ranOn({ state: "FACTORY", factory: run({ instance: "example-1", acceptedAt: "2026-11-22T10:00:01Z" }) })).toBeNull()
    expect(ranOn({ state: "CANCELLED", factory: run({ instance: "example-1" }) })).toBeNull()
    expect(ranOn({ state: "CANCELLED", factory: run() })).toBeNull()
    expect(ranOn({ state: "DONE", factory: null })).toBeNull()
  })
})

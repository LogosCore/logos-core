import { afterEach, describe, expect, it, vi } from "vitest"

// These tests run in plain node (see vitest.config.ts — no jsdom), so there is
// no global Worker. That is the same condition the client has to survive in a
// browser whose worker fails to start, and it is the path every test in this
// repo exercises implicitly: the topology settles inline.

const ORIGINAL_WORKER = (globalThis as { Worker?: unknown }).Worker

afterEach(() => {
  if (ORIGINAL_WORKER === undefined) {
    delete (globalThis as { Worker?: unknown }).Worker
  } else {
    ;(globalThis as { Worker?: unknown }).Worker = ORIGINAL_WORKER
  }
  vi.resetModules()
})

const INPUT = {
  nodes: [{ id: "a", r: 60, width: 180, height: 64 }],
  edges: [],
  dense: false,
}

describe("requestSettle without a usable worker", () => {
  it("resolves null when Worker does not exist", async () => {
    expect(typeof (globalThis as { Worker?: unknown }).Worker).toBe("undefined")
    const { requestSettle } = await import("@/lib/topology/settle-client")

    // null is the caller's signal to settle inline. Resolving (rather than
    // rejecting or hanging) is the contract: a topology must still render.
    await expect(requestSettle(INPUT)).resolves.toBeNull()
  })

  it("resolves null when the Worker constructor throws", async () => {
    ;(globalThis as { Worker?: unknown }).Worker = class {
      constructor() {
        throw new Error("blocked by CSP")
      }
    }
    const { requestSettle } = await import("@/lib/topology/settle-client")

    await expect(requestSettle(INPUT)).resolves.toBeNull()
    // Having failed once, it must not try again on every rebuild — a second
    // throwing construction per settle would be pure cost.
    await expect(requestSettle(INPUT)).resolves.toBeNull()
  })

  it("releases every in-flight request when the worker errors", async () => {
    // A worker-level error delivers no per-request reply, so anything in flight
    // has to be released or those callers wait forever and the map never paints.
    let instance: { onerror?: () => void } | null = null
    ;(globalThis as { Worker?: unknown }).Worker = class {
      onmessage: unknown = null
      onerror: (() => void) | null = null
      constructor() {
        instance = this as unknown as { onerror?: () => void }
      }
      postMessage() {}
      terminate() {}
    }
    const { requestSettle } = await import("@/lib/topology/settle-client")

    const first = requestSettle(INPUT)
    const second = requestSettle(INPUT)
    expect(instance).not.toBeNull()
    instance!.onerror?.()

    await expect(first).resolves.toBeNull()
    await expect(second).resolves.toBeNull()
  })

  it("passes worker positions through and drops a failed settle", async () => {
    type Handler = (e: { data: unknown }) => void
    let onmessage: Handler | null = null
    const sent: { id: number }[] = []
    ;(globalThis as { Worker?: unknown }).Worker = class {
      set onmessage(h: Handler) {
        onmessage = h
      }
      onerror: unknown = null
      postMessage(m: { id: number }) {
        sent.push(m)
      }
      terminate() {}
    }
    const { requestSettle } = await import("@/lib/topology/settle-client")

    const ok = requestSettle(INPUT)
    onmessage!({
      data: { id: sent[0].id, ok: true, positions: [{ id: "a", x: 1, y: 2 }] },
    })
    await expect(ok).resolves.toEqual([{ id: "a", x: 1, y: 2 }])

    const failed = requestSettle(INPUT)
    onmessage!({ data: { id: sent[1].id, ok: false, error: "boom" } })
    // A settle that threw inside the worker falls back to inline, same as a
    // missing worker — the caller should not be handed a partial map.
    await expect(failed).resolves.toBeNull()
  })

  it("ignores a reply for a request nobody is waiting on", async () => {
    type Handler = (e: { data: unknown }) => void
    let onmessage: Handler | null = null
    ;(globalThis as { Worker?: unknown }).Worker = class {
      set onmessage(h: Handler) {
        onmessage = h
      }
      onerror: unknown = null
      postMessage() {}
      terminate() {}
    }
    const { requestSettle } = await import("@/lib/topology/settle-client")
    void requestSettle(INPUT)

    // An id the client never issued (or already resolved) must be a no-op, not
    // a crash — a superseded lens switch produces exactly this.
    expect(() =>
      onmessage!({ data: { id: 9999, ok: true, positions: [] } }),
    ).not.toThrow()
  })
})

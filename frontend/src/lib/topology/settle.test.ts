import { describe, expect, it } from "vitest"
import { settleTopology, type SettleInput } from "@/lib/topology/settle"

// A small hub-and-spoke plus a detached pair — enough to exercise seeding,
// both tick phases, collision and the uncross pass, and to have more than one
// connected component.
function input(overrides: Partial<SettleInput> = {}): SettleInput {
  const ids = ["hub", "a", "b", "c", "d", "lone1", "lone2"]
  return {
    nodes: ids.map((id) => ({ id, r: 60, width: 180, height: 64 })),
    edges: [
      { source: "hub", target: "a" },
      { source: "hub", target: "b" },
      { source: "hub", target: "c" },
      { source: "hub", target: "d" },
      { source: "lone1", target: "lone2" },
    ],
    dense: false,
    ...overrides,
  }
}

describe("settleTopology", () => {
  it("returns one finite position per node, in input order", () => {
    const given = input()
    const out = settleTopology(given)

    expect(out.map((p) => p.id)).toEqual(given.nodes.map((n) => n.id))
    for (const p of out) {
      expect(Number.isFinite(p.x)).toBe(true)
      expect(Number.isFinite(p.y)).toBe(true)
    }
  })

  it("is deterministic for the same input", () => {
    // The map must not reshuffle between reloads of the same data. This is the
    // property that lets the settle move to a worker at all: the caller applies
    // positions computed elsewhere and has to get the same picture.
    expect(settleTopology(input())).toEqual(settleTopology(input()))
  })

  it("separates linked nodes to roughly the link distance", () => {
    const out = settleTopology(input())
    const at = (id: string) => out.find((p) => p.id === id)!
    const hub = at("hub")

    // r + r + LINK_SLACK = 60 + 60 + 60 = 180 is the target; the settle is a
    // relaxation, not a solver, so this asserts the order of magnitude rather
    // than the exact value — it fails loudly if the settle never ran.
    for (const id of ["a", "b", "c", "d"]) {
      const n = at(id)
      const d = Math.hypot(n.x - hub.x, n.y - hub.y)
      expect(d).toBeGreaterThan(90)
      expect(d).toBeLessThan(600)
    }
  })

  it("never leaves two nodes on top of each other", () => {
    const out = settleTopology(input())
    for (let i = 0; i < out.length; i++) {
      for (let j = i + 1; j < out.length; j++) {
        const d = Math.hypot(out[i].x - out[j].x, out[i].y - out[j].y)
        expect(d).toBeGreaterThan(1)
      }
    }
  })

  it("ignores self-edges", () => {
    const withSelf = input()
    withSelf.edges = [...withSelf.edges, { source: "hub", target: "hub" }]
    // A self-edge has no meaning for either the link force or the uncross pass;
    // seedRadial already skipped them, so the layout must be unchanged.
    expect(settleTopology(withSelf)).toEqual(settleTopology(input()))
  })

  it("handles an empty graph and a single node", () => {
    expect(settleTopology({ nodes: [], edges: [], dense: false })).toEqual([])
    const one = settleTopology({
      nodes: [{ id: "solo", r: 30, width: 90, height: 34 }],
      edges: [],
      dense: false,
    })
    expect(one).toHaveLength(1)
    expect(Number.isFinite(one[0].x)).toBe(true)
  })

  it("spreads the dense lens further than the default tuning", () => {
    // The dense branch runs a stronger charge and more link slack. Compare the
    // mean distance from the origin so the assertion tracks the tuning's intent
    // rather than any one node's landing spot.
    const spread = (dense: boolean) => {
      const out = settleTopology(input({ dense }))
      return out.reduce((a, p) => a + Math.hypot(p.x, p.y), 0) / out.length
    }
    expect(spread(true)).toBeGreaterThan(spread(false))
  })
})

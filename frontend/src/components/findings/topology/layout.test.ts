import { describe, expect, it } from "vitest"
import type { Topology } from "@/lib/topology/derive"
import {
  layoutTopology,
  settleInputFor,
} from "@/components/findings/topology/layout"

// Subnet-and-phantom-gateway graph: two kinds of node with different sizes, and
// an edge kind the layout renders, without needing a full HostFieldsFragment.
function topology(): Topology {
  return {
    nodes: [
      { kind: "phantom-subnet", id: "s1", cidr: "10.0.0.0/24" },
      { kind: "phantom-subnet", id: "s2", cidr: "10.0.1.0/24" },
      { kind: "phantom-gateway", id: "g1", ip: "10.0.0.1" },
    ],
    edges: [
      {
        kind: "pivot-unknown",
        id: "e1",
        source: "g1",
        target: "s1",
        isDefault: true,
        destLabel: null,
      },
    ],
    stats: {
      hosts: 0,
      subnets: 2,
      pivots: 0,
      phantomGateways: 1,
      phantomSubnets: 2,
      identities: 0,
      phantomHosts: 0,
    },
  }
}

describe("settleInputFor", () => {
  it("describes exactly the nodes layoutTopology will position", () => {
    // The worker is fed from this function and its answer is applied by
    // layoutTopology. If the two ever disagreed on the node set, a reply would
    // be rejected by the coverage check and every rebuild would silently settle
    // on the main thread — the regression this pins.
    const t = topology()
    const input = settleInputFor(t)
    const { simNodeById } = layoutTopology(t)

    expect(input.nodes.map((n) => n.id)).toEqual([...simNodeById.keys()])
    for (const n of input.nodes) {
      const sim = simNodeById.get(n.id)!
      expect({ r: n.r, width: n.width, height: n.height }).toEqual({
        r: sim.r,
        width: sim.width,
        height: sim.height,
      })
    }
  })

  it("drops self-edges and flags the dense login lens", () => {
    const t = topology()
    t.edges = [
      ...t.edges,
      {
        kind: "pivot-unknown",
        id: "self",
        source: "g1",
        target: "g1",
        isDefault: false,
        destLabel: null,
      },
    ]
    expect(settleInputFor(t).edges).toEqual([{ source: "g1", target: "s1" }])
    expect(settleInputFor(t).dense).toBe(false)

    const withIdentity = topology()
    withIdentity.nodes = [
      ...withIdentity.nodes,
      { kind: "identity", id: "u1", user: "root", wellKnown: true },
    ]
    expect(settleInputFor(withIdentity).dense).toBe(true)
  })
})

describe("layoutTopology pre-settled positions", () => {
  it("places nodes exactly where a complete pre-settle says, with no ticks", () => {
    const t = topology()
    const presettled = new Map([
      ["s1", { x: 100, y: 200 }],
      ["s2", { x: -300, y: 50 }],
      ["g1", { x: 0, y: -400 }],
    ])
    const { simNodeById } = layoutTopology(t, presettled)

    // Exact equality is the assertion: any tick at all would move these, and
    // the whole point of the worker path is that the main thread runs none.
    for (const [id, p] of presettled) {
      const sim = simNodeById.get(id)!
      expect({ x: sim.x, y: sim.y }).toEqual(p)
    }
  })

  it("settles inline when the pre-settle misses a node", () => {
    const t = topology()
    // A reply computed for an older graph. Trusting it would leave g1 unplaced,
    // and d3 would drop it at its phyllotaxis default in the middle of the map.
    const stale = new Map([
      ["s1", { x: 100, y: 200 }],
      ["s2", { x: -300, y: 50 }],
    ])
    const { simNodeById } = layoutTopology(t, stale)

    expect(simNodeById.get("g1")?.x).toBeDefined()
    // Having settled inline, it must NOT have honoured the partial map.
    expect(simNodeById.get("s1")?.x).not.toBe(100)
  })

  it("matches the inline result when handed that result back", () => {
    // Worker path and inline path have to agree: settling inline, then feeding
    // those positions in as if a worker had produced them, must land the same
    // map.
    const t = topology()
    const inline = layoutTopology(t)
    const positions = new Map(
      [...inline.simNodeById.values()].map((n) => [
        n.id,
        { x: n.x!, y: n.y! },
      ]),
    )
    const viaWorker = layoutTopology(t, positions)

    for (const [id, sim] of inline.simNodeById) {
      const other = viaWorker.simNodeById.get(id)!
      expect({ x: other.x, y: other.y }).toEqual({ x: sim.x, y: sim.y })
    }
    expect(viaWorker.nodes.map((n) => n.position)).toEqual(
      inline.nodes.map((n) => n.position),
    )
  })

  it("hands back a simulation cooled to rest, not a hot one", () => {
    // A simulation that was never ticked keeps alpha at 1, and per-tick
    // displacement scales with alpha. The drag re-heat targets 0.2, so an
    // uncooled simulation would make the first grab fling the graph apart.
    for (const layout of [
      layoutTopology(topology()),
      layoutTopology(
        topology(),
        new Map([
          ["s1", { x: 10, y: 20 }],
          ["s2", { x: -30, y: 5 }],
          ["g1", { x: 0, y: -40 }],
        ]),
      ),
    ]) {
      expect(layout.simulation.alpha()).toBeLessThanOrEqual(
        layout.simulation.alphaMin(),
      )
    }
  })
})

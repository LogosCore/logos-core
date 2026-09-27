// The force settle, extracted from the layout so it can run off the main thread.
//
// This is the expensive half of building a topology map: radial BFS seeding,
// 300–410 synchronous d3-force ticks in two phases, then the crossing-reduction
// pass. It is also pure geometry — ids, radii, sizes and edge pairs in,
// positions out — with no DOM, no React and no React Flow, which is exactly
// what makes it movable to a worker (see settle.worker.ts).
//
// topology-view.tsx already wrapped the rebuild in useDeferredValue because the
// chunk was "noticeable on the dense users lens". That keeps the click from
// feeling dropped, but the work still ran on the main thread — React just chose
// a later moment to block in. On a weak CPU the freeze was the problem, not its
// timing. Running it in a worker removes it from the main thread entirely.
//
// Everything here is deterministic for a given input order, which is a
// documented property of the map (it must not reshuffle between reloads of the
// same data). Keep it that way: no Date, no Math.random beyond what d3's own
// jiggle does for coincident nodes, and no iteration over unordered collections.

import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from "d3-force"
import { seedRadial } from "@/lib/topology/seed"
import { reduceCrossings } from "@/lib/topology/untangle"

// The settle runs in two phases. Collision is the classic force-layout trap:
// uncrossing two arms of the graph requires nodes to pass THROUGH each other,
// and forceCollide forbids exactly that move — enabled from tick 1 it freezes
// early crossings into the cooled layout (a local minimum no amount of extra
// ticks escapes, since per-tick displacement scales with the decaying alpha).
// So phase 1 settles topology with links/charge only, letting the graph
// unwind freely; phase 2 re-heats moderately, adds collision, and pushes the
// remaining overlaps apart without disturbing the untangled shape.
const UNTANGLE_TICKS = 150
const POLISH_TICKS = 150
// A moderate re-heat: enough energy to push the remaining overlaps apart
// without disturbing the untangled shape. The drag re-heat (use-simulation)
// runs cooler (0.2) and localized to the grabbed node's neighborhood, so
// grabbing a node perturbs the map less than this one-time settle does.
const POLISH_ALPHA = 0.3
const LINK_SLACK = 60 // breathing room added to every link beyond node radii
const CHARGE_STRENGTH = -1200
const COLLIDE_PADDING = 16
const CENTERING_STRENGTH = 0.06 // weak pull keeps disconnected pieces nearby

// The users lens is a dense bipartite mesh (shared accounts wire many hosts
// together), not the tree-ish hub-and-spoke the subnet lens is. It needs more
// room to breathe and a longer untangle phase before collision freezes the
// shape, so it runs hotter than the network lenses.
const DENSE_CHARGE_STRENGTH = -2200
const DENSE_LINK_SLACK = 110
const DENSE_UNTANGLE_TICKS = 260

/** Minimal node the settle needs. Structured-cloneable on purpose — this
 *  crosses a worker boundary. */
export interface SettleNode {
  id: string
  r: number
  width: number
  height: number
}

/** An undirected edge between two node ids. Self-edges are the caller's to
 *  filter; they are meaningless to both the link force and the uncross pass. */
export interface SettleEdge {
  source: string
  target: string
}

export interface SettleInput {
  nodes: SettleNode[]
  edges: SettleEdge[]
  /** Run the hotter, roomier dense-mesh tuning (the users/identities lens). */
  dense: boolean
}

export interface SettledPosition {
  id: string
  x: number
  y: number
}

// d3 mutates the node objects it is given, so the simulation runs on internal
// copies and only positions are handed back. That keeps the contract value-like
// in both directions, which a worker boundary requires anyway.
type SimNode = SimulationNodeDatum & SettleNode

/**
 * Seeds, settles and uncrosses the graph. Returns one position per input node,
 * in input order.
 */
export function settleTopology(input: SettleInput): SettledPosition[] {
  const { nodes, edges, dense } = input

  const simNodes: SimNode[] = nodes.map((n) => ({
    id: n.id,
    r: n.r,
    width: n.width,
    height: n.height,
  }))

  const chargeStrength = dense ? DENSE_CHARGE_STRENGTH : CHARGE_STRENGTH
  const linkSlack = dense ? DENSE_LINK_SLACK : LINK_SLACK
  const untangleTicks = dense ? DENSE_UNTANGLE_TICKS : UNTANGLE_TICKS

  const realEdges = edges.filter((e) => e.source !== e.target)
  const links: SimulationLinkDatum<SimNode>[] = realEdges.map((e) => ({
    source: e.source,
    target: e.target,
  }))

  // Pre-set positions so the simulation starts from an untangled radial shape
  // instead of d3's input-order spiral (forceSimulation only auto-places
  // nodes whose x/y are unset). The settle below then relaxes distances
  // rather than untangling topology.
  const seeded = seedRadial(simNodes, realEdges)
  for (const sim of simNodes) {
    const p = seeded.get(sim.id)
    if (p) {
      sim.x = p.x
      sim.y = p.y
    }
  }

  // Stopped immediately so d3's internal timer never runs on its own; the
  // synchronous ticks below do all the settling.
  const simulation = forceSimulation(simNodes)
    .force(
      "link",
      forceLink<SimNode, SimulationLinkDatum<SimNode>>(links)
        .id((d) => d.id)
        .distance((l) => {
          const s = l.source as SimNode
          const t = l.target as SimNode
          return s.r + t.r + linkSlack
        }),
    )
    .force("charge", forceManyBody().strength(chargeStrength))
    .force("x", forceX(0).strength(CENTERING_STRENGTH))
    .force("y", forceY(0).strength(CENTERING_STRENGTH))
    .stop()

  // Phase 1: untangle (no collision — see the tick constants above).
  simulation.tick(untangleTicks)

  // Phase 2: polish. Attaching a force to a live simulation initializes it with
  // the current nodes (documented d3 behavior). alpha() only resets the cooling
  // variable the synchronous tick() loop reads — the internal timer stays
  // stopped, so no restart() here.
  simulation
    .force(
      "collide",
      forceCollide<SimNode>().radius((d) => d.r + COLLIDE_PADDING),
    )
    .alpha(POLISH_ALPHA)
  simulation.tick(POLISH_TICKS)

  // Phase 3: the global uncross the springs can't do. The settle leaves
  // local-minimum crossings frozen in (collision forbids the pass-through move
  // that would fix them); this relocates nodes to strictly cut edge crossings.
  reduceCrossings(
    simNodes,
    realEdges.map((e) => ({ a: e.source, b: e.target })),
  )

  // The simulation is discarded here. The live instance the drag re-heat needs
  // is built on the main thread from these positions, with zero ticks — see
  // layoutTopology's `presettled` path.
  simulation.stop()

  return simNodes.map((n) => ({ id: n.id, x: n.x ?? 0, y: n.y ?? 0 }))
}

/** Collision padding is shared with the live simulation the main thread builds,
 *  so the drag physics match the settle that produced the positions. */
export const SETTLE_COLLIDE_PADDING = COLLIDE_PADDING
export const SETTLE_CENTERING_STRENGTH = CENTERING_STRENGTH
export const SETTLE_LINK_SLACK = LINK_SLACK
export const SETTLE_DENSE_LINK_SLACK = DENSE_LINK_SLACK
export const SETTLE_CHARGE_STRENGTH = CHARGE_STRENGTH
export const SETTLE_DENSE_CHARGE_STRENGTH = DENSE_CHARGE_STRENGTH

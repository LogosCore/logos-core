import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from "d3-force"
import { MarkerType, type Edge, type Node } from "@xyflow/react"
import type { TopoEdge, TopoNode, Topology } from "@/lib/topology/derive"
import {
  SETTLE_CENTERING_STRENGTH,
  SETTLE_CHARGE_STRENGTH,
  SETTLE_COLLIDE_PADDING,
  SETTLE_DENSE_CHARGE_STRENGTH,
  SETTLE_DENSE_LINK_SLACK,
  SETTLE_LINK_SLACK,
  settleTopology,
  type SettleInput,
} from "@/lib/topology/settle"

// Maps the framework-free topology model onto React Flow nodes/edges and lays
// it out. Subnet-as-node design: subnets are compact hub pills, hosts are
// cards, and EVERY interface is an explicit labeled edge from host to subnet.
// There is no containment — a multi-homed host connects to all of its subnets
// symmetrically, which is exactly the property that makes it a pivot, so no
// interface is privileged over another.
//
// Positioning is force-directed (d3-force), not hierarchical: the topology is
// a mesh of hubs and spokes, and a layered algorithm (dagre) funnels every
// edge through one corridor between rank columns, piling labels on top of
// each other. Forces instead pull linked nodes to a comfortable distance and
// push everything else apart, so hubs become stars and a dual-homed host
// settles between its two subnets. The simulation is pre-settled synchronously
// to a fixed tick count, so the first paint is already a finished map — but
// the simulation instance is returned alive (paused, alpha cooled) so the view
// can re-heat it on drag for Obsidian-style live physics (see use-simulation).
// Initial positions come from radial BFS seeding (see lib/topology/seed.ts),
// not d3's phyllotaxis spiral, so the settle starts from an already-untangled
// shape. Everything is deterministic for a given node order — the map doesn't
// reshuffle between reloads of the same data.

const HOST_W = 180
const HOST_H = 64
const SUBNET_H = 36
const PHANTOM_W = 160
const PHANTOM_H = 56
// Unknown-source nodes are a compact single-row pill, smaller than the
// two-line phantom gateway/subnet cards.
const PHANTOM_HOST_W = 150
const PHANTOM_HOST_H = 34

// Node types rendered as a full pill (rounded-full) rather than a card: they
// need explicit dimensions from the layout AND a matching focus-ring radius
// (see emphasis.ts). One list so a new pill type can't be added to one place
// and forgotten in the other.
const PILL_NODE_TYPES = new Set(["subnet", "identity"])
export const isPillNodeType = (type: string | undefined): boolean =>
  type !== undefined && PILL_NODE_TYPES.has(type)

// Node dimensions must be known before render (collision radii, center →
// top-left conversion), so the pill width is estimated from its label —
// monospace CIDR + host count, padding, and the leading icon.
const SUBNET_CHAR_W = 7.5
const SUBNET_EXTRA_W = 64 // horizontal padding + icon + gaps
const SUBNET_MIN_W = 150

// Identity pill (users lens). Same pill geometry as a subnet, sized from the
// username so the collision radius is right before render.
const IDENTITY_CHAR_W = 7
const IDENTITY_EXTRA_W = 52 // padding + user icon + gaps
const IDENTITY_MIN_W = 96
const IDENTITY_H = 36

// Leaf-subnets list node. Rows beyond the cap render as "+k more" (the full
// list lives in the tooltip), so the node — and its collision radius — stays
// bounded no matter how many tunnels a concentrator carries.
export const LEAF_SUBNET_MAX_ROWS = 6
const LEAF_CHAR_W = 6.8 // 11px monospace row text
const LEAF_EXTRA_W = 34 // px-3 padding + border + truncation slack
const LEAF_MIN_W = 170
const LEAF_ROW_H = 18.5 // 11px row + gap-0.5
const LEAF_FRAME_H = 40 // py-2 + border + header row

// The users lens is a dense bipartite mesh (shared accounts wire many hosts
// together), not the tree-ish hub-and-spoke the subnet lens is. It needs more
// room to breathe, so it runs hotter than the network lenses — which stay on
// the tuned defaults the user is happy with. Detected by the presence of any
// login-layer node so no lens flag has to be threaded through the layout.
//
// The tick counts and force strengths themselves live in lib/topology/settle.ts
// now, shared with the worker; this file only decides which set applies and
// rebuilds the same forces for the live drag simulation.
const LOGIN_NODE_KINDS = new Set<TopoNode["kind"]>([
  "identity",
  "local-identities",
  "lone-sources",
  "phantom-host",
])

function isDenseLoginLens(nodes: ReadonlyArray<TopoNode>): boolean {
  return nodes.some((n) => LOGIN_NODE_KINDS.has(n.kind))
}

// Shared look for the iface/route text riding on edges. SVG text doesn't
// inherit the page color — without an explicit themed fill it renders black,
// which vanishes against the dark theme's label pill.
const MONO_LABEL = {
  fontSize: 10,
  fontFamily: "monospace",
  fill: "var(--color-foreground)",
} as const

// The dense users lens reads as a hairball when every login edge carries its
// full stroke. So login edges render QUIET at rest — a neutral grey at reduced
// opacity (FloatingEdge applies it from this data flag) — and only snap to
// their real color when focus/search lights them (see emphasis.ts). The strong
// color stays in `style.stroke`; this is just the resting override.
const LOGIN_REST_STROKE = "var(--color-muted-foreground)"

// Baseline z for every node. Edges top out at z 0 (see floating-edge / the
// React Flow z-index model), and an unstyled node also defaults to 0 — a tie
// that only DOM order breaks, and that a dimmed (near-transparent) node loses
// visually to any edge crossing it. Pinning nodes one level up guarantees
// edges always sit beneath them; a selected node still elevates above this.
const NODE_Z = 1

function subnetWidth(cidr: string, hostCount: number) {
  const label = `${cidr} · ${hostCount}`
  return Math.max(SUBNET_MIN_W, Math.ceil(label.length * SUBNET_CHAR_W) + SUBNET_EXTRA_W)
}

function identityWidth(user: string) {
  return Math.max(
    IDENTITY_MIN_W,
    Math.ceil(user.length * IDENTITY_CHAR_W) + IDENTITY_EXTRA_W,
  )
}

function leafSubnetsSize(entries: { cidr: string; iface: string }[]) {
  const longestRow = entries.reduce(
    (max, e) => Math.max(max, `${e.iface} · ${e.cidr}`.length),
    0,
  )
  const visibleRows =
    Math.min(entries.length, LEAF_SUBNET_MAX_ROWS) +
    (entries.length > LEAF_SUBNET_MAX_ROWS ? 1 : 0) // the "+k more" row
  return {
    width: Math.max(LEAF_MIN_W, Math.ceil(longestRow * LEAF_CHAR_W) + LEAF_EXTRA_W),
    height: LEAF_FRAME_H + Math.ceil(visibleRows * LEAF_ROW_H),
  }
}

// A framed string-list node (users lens) — the lone-sources (ghost origins) and
// local-identities (single-host accounts) pills, both one row per entry capped
// with a "+k more" row. Same geometry as leaf-subnets, sized from plain row
// strings, so the node and its collision radius stay bounded however many
// entries collapse into it.
function framedListSize(rows: string[]) {
  const longestRow = rows.reduce((max, r) => Math.max(max, r.length), 0)
  const visibleRows =
    Math.min(rows.length, LEAF_SUBNET_MAX_ROWS) +
    (rows.length > LEAF_SUBNET_MAX_ROWS ? 1 : 0)
  return {
    width: Math.max(LEAF_MIN_W, Math.ceil(longestRow * LEAF_CHAR_W) + LEAF_EXTRA_W),
    height: LEAF_FRAME_H + Math.ceil(visibleRows * LEAF_ROW_H),
  }
}

function sizeOf(n: TopoNode): { width: number; height: number } {
  switch (n.kind) {
    case "host":
      return { width: HOST_W, height: HOST_H }
    case "subnet":
      return { width: subnetWidth(n.cidr, n.hostIds.length), height: SUBNET_H }
    case "leaf-subnets":
      return leafSubnetsSize(n.entries)
    case "lone-sources":
      return framedListSize(n.sources.map((s) => s.label))
    case "local-identities":
      return framedListSize(n.users)
    case "identity":
      return { width: identityWidth(n.user), height: IDENTITY_H }
    case "phantom-host":
      return { width: PHANTOM_HOST_W, height: PHANTOM_HOST_H }
    case "phantom-gateway":
    case "phantom-subnet":
      return { width: PHANTOM_W, height: PHANTOM_H }
  }
}

// Half-diagonal of the node's rectangle — the radius that fully contains it,
// used for collision and link-length math.
function radiusOf(size: { width: number; height: number }) {
  return Math.hypot(size.width, size.height) / 2
}

function nodeData(n: TopoNode): Node["data"] {
  switch (n.kind) {
    case "host":
      return { host: n.host }
    case "subnet":
      return { cidr: n.cidr, hostCount: n.hostIds.length }
    case "phantom-gateway":
      return { ip: n.ip }
    case "phantom-subnet":
      return { cidr: n.cidr }
    case "leaf-subnets":
      return { entries: n.entries }
    case "lone-sources":
      return { sources: n.sources }
    case "local-identities":
      return { users: n.users }
    case "identity":
      return { user: n.user, wellKnown: n.wellKnown }
    case "phantom-host":
      return { label: n.label }
  }
}

function edgeOf(e: TopoEdge): Edge {
  const base = { id: e.id, source: e.source, target: e.target }
  switch (e.kind) {
    case "membership":
      return {
        ...base,
        label: e.iface ? `${e.iface} · ${e.ip}` : e.ip,
        style: { stroke: "var(--color-border)", strokeWidth: 1.5 },
        labelStyle: { ...MONO_LABEL, fill: "var(--color-muted-foreground)" },
      }
    case "membership-group":
      // Unlabeled on purpose: the iface/ip detail lives inside the
      // leaf-subnets node this edge points at.
      return {
        ...base,
        style: { stroke: "var(--color-border)", strokeWidth: 1.5 },
      }
    case "pivot":
      return {
        ...base,
        label: e.destLabel ?? undefined,
        markerEnd: { type: MarkerType.ArrowClosed },
        animated: !e.isDefault,
        style: {
          stroke: "var(--color-primary)",
          strokeWidth: 2,
          strokeDasharray: e.isDefault ? "6 4" : undefined,
        },
        labelStyle: MONO_LABEL,
      }
    case "pivot-unknown":
      return {
        ...base,
        label: e.destLabel ?? undefined,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: {
          stroke: "var(--color-amber-500, #f59e0b)",
          strokeWidth: 2,
          strokeDasharray: "6 4",
        },
        labelStyle: MONO_LABEL,
      }
    case "reaches":
      return {
        ...base,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: {
          stroke: "var(--color-sky-500, #0ea5e9)",
          strokeWidth: 1.5,
          strokeDasharray: "4 4",
        },
      }
    case "logged-into":
      // identity → host: the account lands on the host. Direction is carried by
      // the arrowhead, not a marching-ants animation: the users lens animated
      // every login edge, and on a dense graph hundreds of infinitely-animating
      // SVG paths repaint every frame at idle (fans spin up). Static + arrow.
      return {
        ...base,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: { stroke: "var(--color-primary)", strokeWidth: 2 },
        data: { restStroke: LOGIN_REST_STROKE },
      }
    case "logged-from":
    case "logged-from-group":
    case "local-group":
      // The quiet half of the login wiring, all rendered identically: muted grey
      // (vs the primary "logged into") so the origin reads as the quieter side,
      // arrowhead for direction, not animated (see "logged-into"). Covers the raw
      // source-host → identity edge, the merged lone-sources → identity edge, and
      // the merged local-identities → host edge. Quiet at rest; snaps to color
      // when an endpoint is focused/searched.
      return {
        ...base,
        markerEnd: { type: MarkerType.ArrowClosed },
        style: {
          stroke: "var(--color-muted-foreground)",
          strokeWidth: 1.5,
        },
        data: { restStroke: LOGIN_REST_STROKE },
      }
  }
}

// width/height ride on the sim node so the live loop can convert the
// simulation's center coordinates to React Flow's top-left (and back) without
// a separate size lookup.
export type SimNode = SimulationNodeDatum & {
  id: string
  r: number
  width: number
  height: number
}

type TopologySimulation = Simulation<SimNode, SimulationLinkDatum<SimNode>>

export type TopologyLayout = {
  nodes: Node[]
  edges: Edge[]
  simulation: TopologySimulation
  simNodeById: Map<string, SimNode>
}

/**
 * The settle's view of a topology: one entry per node with the size and radius
 * the layout will use, plus the edges and the dense-lens flag.
 *
 * Exported so the worker is fed from the same code path layoutTopology uses.
 * Deriving the node list twice, independently, is how a worker result ends up
 * not matching the graph it gets applied to — and sizeOf/radiusOf are the exact
 * functions that would drift.
 */
export function settleInputFor(topology: Topology): SettleInput {
  const { nodes: topoNodes, edges: topoEdges } = topology
  return {
    nodes: topoNodes.map((n) => {
      const size = sizeOf(n)
      return { id: n.id, r: radiusOf(size), ...size }
    }),
    edges: topoEdges
      .filter((e) => e.source !== e.target)
      .map((e) => ({ source: e.source, target: e.target })),
    dense: isDenseLoginLens(topoNodes),
  }
}

/**
 * Builds the React Flow nodes/edges and the live (paused) simulation for a
 * topology.
 *
 * `presettled` carries positions already computed by the settle — normally in a
 * worker (see lib/topology/settle.worker.ts). Given them, this function runs
 * zero ticks: it seeds the simulation at those positions, attaches the same
 * forces so a drag re-heat behaves identically, and leaves it stopped. That
 * makes the main-thread cost of a rebuild the cheap half only.
 *
 * Without them it settles inline, which is the old behavior and the fallback
 * when a worker is unavailable (no `Worker` at all, or one that failed to
 * start).
 */
export function layoutTopology(
  topology: Topology,
  presettled?: ReadonlyMap<string, { x: number; y: number }>,
): TopologyLayout {
  const { nodes: topoNodes, edges: topoEdges } = topology

  const sizeById = new Map<string, { width: number; height: number }>()
  for (const n of topoNodes) {
    sizeById.set(n.id, sizeOf(n))
  }

  // --- force simulation -------------------------------------------------------
  const simNodes: SimNode[] = topoNodes.map((n) => {
    const size = sizeById.get(n.id)!
    return { id: n.id, r: radiusOf(size), ...size }
  })
  const simNodeById = new Map(simNodes.map((n) => [n.id, n]))

  const realEdges = topoEdges.filter((e) => e.source !== e.target)
  const links: SimulationLinkDatum<SimNode>[] = realEdges.map((e) => ({
    source: e.source,
    target: e.target,
  }))

  // Hotter, roomier tuning for the dense login mesh; defaults elsewhere.
  const dense = isDenseLoginLens(topoNodes)
  const chargeStrength = dense
    ? SETTLE_DENSE_CHARGE_STRENGTH
    : SETTLE_CHARGE_STRENGTH
  const linkSlack = dense ? SETTLE_DENSE_LINK_SLACK : SETTLE_LINK_SLACK

  // Positions come either from the worker or from settling right here. Either
  // way the simulation below starts from a finished layout, so the first paint
  // is the finished map.
  //
  // A worker result computed for a different graph must not be trusted: a node
  // missing from it is left unplaced, and d3 would then drop it at its
  // phyllotaxis default in the middle of the map. Falling back to an inline
  // settle when the coverage does not match keeps a stale reply from producing a
  // wrong picture rather than a slow one.
  const covers =
    presettled !== undefined &&
    simNodes.every((n) => presettled.has(n.id))
  const positions = covers
    ? presettled!
    : new Map(
        settleTopology({
          nodes: simNodes.map((n) => ({
            id: n.id,
            r: n.r,
            width: n.width,
            height: n.height,
          })),
          edges: realEdges.map((e) => ({ source: e.source, target: e.target })),
          dense,
        }).map((p) => [p.id, { x: p.x, y: p.y }]),
      )

  for (const sim of simNodes) {
    const p = positions.get(sim.id)
    if (p) {
      sim.x = p.x
      sim.y = p.y
    }
  }

  // Stopped immediately so d3's internal timer never runs on its own. No ticks
  // here: the nodes are already at their settled positions, and the forces exist
  // only so a drag can re-heat them. Collision is attached up front (the settle
  // adds it for its second phase) so drag physics match what produced the
  // layout.
  const simulation: TopologySimulation = forceSimulation(simNodes)
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
    .force("x", forceX(0).strength(SETTLE_CENTERING_STRENGTH))
    .force("y", forceY(0).strength(SETTLE_CENTERING_STRENGTH))
    .force(
      "collide",
      forceCollide<SimNode>().radius((d) => d.r + SETTLE_COLLIDE_PADDING),
    )
    .stop()

  // Cool it to rest. stop() only halts the timer — alpha stays at its initial 1
  // on a simulation that was never ticked, and every tick's displacement scales
  // with alpha. The drag re-heat does `alphaTarget(0.2).restart()`, so a
  // simulation left at alpha 1 would start the first drag at five times the
  // intended energy and fling the whole neighborhood apart on grab. Back when
  // the settle ran here, 300+ ticks had already decayed alpha to ~0.01 and this
  // was free; now that the ticks happen in the worker it has to be explicit.
  //
  // alphaMin is the "at rest" floor d3 stops at, so this is the state a
  // completed settle leaves behind. From there alphaTarget climbs gradually,
  // which is the gentle reheat use-simulation documents.
  simulation.alpha(simulation.alphaMin())

  const nodeType: Record<TopoNode["kind"], string> = {
    host: "host",
    subnet: "subnet",
    "phantom-gateway": "phantomGateway",
    "phantom-subnet": "phantomSubnet",
    "leaf-subnets": "leafSubnets",
    "lone-sources": "loneSources",
    "local-identities": "localIdentities",
    identity: "identity",
    "phantom-host": "phantomHost",
  }

  const rfNodes: Node[] = topoNodes.map((n) => {
    const size = sizeById.get(n.id)!
    const sim = simNodeById.get(n.id)!
    return {
      id: n.id,
      type: nodeType[n.kind],
      // Keep every node above the edge layer (see NODE_Z).
      zIndex: NODE_Z,
      // Simulation positions are node centers; React Flow wants top-left.
      position: { x: (sim.x ?? 0) - size.width / 2, y: (sim.y ?? 0) - size.height / 2 },
      // The subnet pill is fully sized by the layout (rounded-full needs real
      // dimensions); the leaf list gets its width pinned (rows truncate
      // against it) but keeps natural height so rows can never be clipped by
      // an estimate. Other node types size themselves.
      // Pill nodes (subnet, identity) are fully sized by the layout because
      // rounded-full needs real dimensions; the leaf list gets its width pinned
      // (rows truncate against it) but keeps natural height. Others self-size.
      style: isPillNodeType(n.kind)
        ? { width: size.width, height: size.height }
        : n.kind === "leaf-subnets" ||
            n.kind === "lone-sources" ||
            n.kind === "local-identities"
          ? { width: size.width }
          : undefined,
      data: nodeData(n),
      selectable: n.kind === "subnet" ? false : undefined,
    }
  })

  // --- edges -----------------------------------------------------------------
  const rfEdges: Edge[] = topoEdges.map(edgeOf)

  // All edges render as "floating": connection points follow the nodes as the
  // user drags them, instead of sticking to fixed left/right handles.
  return {
    nodes: rfNodes,
    edges: rfEdges.map((e) => ({ ...e, type: "floating" })),
    simulation,
    simNodeById,
  }
}

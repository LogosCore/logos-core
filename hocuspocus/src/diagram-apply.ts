// Apply a diagram (diagram-language.ts) to a drawing.
//
// The agent says what the diagram is; this decides where everything goes and
// writes the elements. What it does, in order, inside the caller's single
// transaction:
//
//   1. checks every reference against the drawing, so a typo in an id is a
//      refusal with its line number rather than a stray shape;
//   2. erases what the text removes;
//   3. updates the nodes and zones that already exist, in place. A card that
//      changes is rebuilt at the same position, keeping its frame's element
//      id so the arrows bound to it stay bound;
//   4. lays out the new nodes with dagre around the ones already there, which
//      never move: an operator's hand-arranged layout survives every agent
//      edit. A new node goes next to the node it is connected to, then moves
//      off anything it would overlap;
//   5. sizes each zone around what it holds;
//   6. draws the connections, bound to both ends and placed by the server.
//
// Merge is the default: anything on the canvas the text does not mention is
// left alone, hand-drawn shapes included. "replace" clears the canvas first.

import dagre from "@dagrejs/dagre";
import type * as Y from "yjs";

import {
  CARD_KINDS,
  ZONE_TITLE_SPACE,
  buildCard,
  buildZone,
  cardWidth,
  plainShapeColors,
  CARD_HEIGHT,
  edgeLook,
  type EdgeStyle,
  type Skeleton,
  type ShapeStyle,
  type ShapeTag,
  type ZoneKind,
} from "./drawing-catalogue.js";
import { DiagramError, defaultHeads, type ArrowKind, type Diagram, type DiagramProblem, type Route } from "./diagram-language.js";
import { layoutArrows, pathCrosses, type Obstacle } from "./arrow-layout.js";
import { readScene, unionBox, type Box, type Scene, type SceneNode, type SceneZone } from "./diagram-read.js";
import { boundTextFields, measureText, normalizeElement, type NormalizedElement } from "./drawing-elements.js";
import { placeBoundArrow, wireBindings } from "./apply-drawing.js";
import { skeletonsToElements } from "./drawing-skeleton.js";

export type DiagramMode = "merge" | "replace";

export interface DiagramResult {
  created: number;
  updated: number;
  removed: number;
  connected: number;
  warnings: string[];
}

// Layers (drawing-order.ts): zones under connections under nodes. A nested
// zone paints over the zone holding it.
const ZONE_Z = -20;
const EDGE_Z = -1;
const NODE_Z = 0;

const ZONE_PAD = 32;
/** How wide an arrow's label may run before it wraps. */
const EDGE_LABEL_WIDTH = 216;
const NODE_GAP = 90;
const RANK_GAP = 150;
const CLEARANCE = 40;

const isCard = (kind: string) => CARD_KINDS.includes(kind);
/** Drawn as a card: a card kind, or a box given an icon. */
const asCard = (kind: string, icon?: string) => isCard(kind) || (kind === "box" && !!icon);

interface Plan {
  id: string;
  kind: string;
  title: string;
  detail: string;
  badges: string[];
  color?: string;
  icon?: string;
  width: number;
  height: number;
  zone?: string;
  /** Set for a node that exists; new nodes get one from the layout. */
  box?: Box;
  existing?: SceneNode;
  /** Something about it differs from the canvas. */
  changed: boolean;
  /** Changed in a way only rebuilding it shows: anything about a card, or
   * a different kind. A hand-drawn shape that only gets new words keeps the
   * look its author gave it. */
  rebuild: boolean;
}

export function applyDiagram(
  elements: Y.Map<NormalizedElement>,
  diagram: Diagram,
  mode: DiagramMode,
): DiagramResult {
  const result: DiagramResult = { created: 0, updated: 0, removed: 0, connected: 0, warnings: [] };
  const live = () => [...elements.values()].filter((el) => el && !el.isDeleted);

  // Checked before anything is written. Y.js does not roll a transaction
  // back when it throws, so a check that failed after "replace" had cleared
  // the canvas would leave it cleared.
  const replacing = mode === "replace";
  check(diagram, replacing ? readScene([]) : readScene(live()), result);
  if (replacing) {
    for (const el of live()) tombstone(elements, el.id);
  }
  let scene = readScene(live());
  const style: ShapeStyle = diagram.style ?? "sketch";

  // --- Removals ---------------------------------------------------------
  for (const removal of diagram.removals) {
    if (removal.kind === "item") {
      const node = scene.nodes.get(removal.id);
      const zone = scene.zones.get(removal.id);
      if (node) {
        for (const id of node.elementIds) tombstone(elements, id);
        for (const edge of scene.edges.filter((e) => e.from === node.id || e.to === node.id)) {
          tombstone(elements, edge.id);
          if (edge.labelId) tombstone(elements, edge.labelId);
        }
        result.removed++;
      } else if (zone) {
        for (const id of zone.elementIds) tombstone(elements, id);
        result.removed++;
      } else {
        result.warnings.push(`line ${removal.line}: nothing called "${removal.id}" to remove`);
      }
    } else {
      const matches = scene.edges.filter(
        (e) => (e.from === removal.from && e.to === removal.to) || (e.from === removal.to && e.to === removal.from),
      );
      for (const edge of matches) {
        tombstone(elements, edge.id);
        if (edge.labelId) tombstone(elements, edge.labelId);
      }
      if (matches.length === 0) {
        result.warnings.push(`line ${removal.line}: no connection between ${removal.from} and ${removal.to} to remove`);
      } else {
        result.removed += matches.length;
      }
    }
  }
  if (diagram.removals.length > 0) scene = readScene(live());

  // --- Nodes: what each should become ------------------------------------
  const plans = new Map<string, Plan>();
  for (const existing of scene.nodes.values()) {
    plans.set(existing.id, {
      id: existing.id,
      kind: existing.kind,
      title: existing.title,
      detail: existing.detail,
      badges: [...existing.badges],
      color: existing.color,
      icon: existing.icon,
      width: existing.box.width,
      height: existing.box.height,
      zone: existing.zone,
      box: existing.box,
      existing,
      changed: false,
      rebuild: false,
    });
  }
  for (const node of diagram.nodes.values()) {
    const prior = plans.get(node.id);
    const kind = node.kind ?? prior?.kind ?? "box";
    // null clears a choice; undefined keeps what is there.
    let color = node.color === null ? undefined : (node.color ?? prior?.color);
    let icon = node.icon === null ? undefined : (node.icon ?? prior?.icon);
    if (icon && !isCard(kind) && kind !== "box") {
      result.warnings.push(`line ${node.line}: only cards and boxes take an icon, and "${node.id}" is a ${kind}; the icon was left off`);
      icon = undefined;
    }
    if (color && kind === "note") {
      result.warnings.push(`line ${node.line}: a note keeps its yellow; the colour was left off`);
      color = undefined;
    }
    const card = asCard(kind, icon);
    const badges = [...new Set([...(prior?.badges ?? []), ...node.addBadges])].filter((b) => !node.removeBadges.includes(b));
    if (badges.length > 0 && !card) {
      result.warnings.push(
        `line ${node.line}: only cards carry badges, and "${node.id}" is a ${kind}; its badges were left off ` +
          `(a box with an icon= is a card)`,
      );
      badges.length = 0;
    }
    const title = node.title ?? prior?.title ?? node.id;
    const detail = card ? (node.detail ?? prior?.detail ?? "") : "";
    const size = nodeSize(kind, title, detail, icon);
    if (prior?.existing) {
      const restyled = color !== prior.color || icon !== prior.icon;
      const changed =
        restyled || kind !== prior.kind || title !== prior.title || detail !== prior.detail || badges.join() !== prior.badges.join();
      if (node.zone && node.zone !== prior.zone) {
        result.warnings.push(
          `line ${node.line}: "${node.id}" is already on the canvas outside zone "${node.zone}" and was not moved; ` +
            `remove it and add it again to put it there`,
        );
      }
      if (!changed) continue;
      const rebuild = card || restyled || kind !== prior.kind || prior.existing.builtin;
      plans.set(node.id, {
        ...prior,
        kind,
        title,
        detail,
        badges,
        color,
        icon,
        changed: true,
        rebuild,
        width: rebuild ? size.width : prior.width,
        height: rebuild ? size.height : prior.height,
      });
    } else {
      plans.set(node.id, { id: node.id, kind, title, detail, badges, color, icon, ...size, zone: node.zone, changed: true, rebuild: true });
    }
  }

  // --- Zones --------------------------------------------------------------
  const zoneParents = new Map<string, string | undefined>();
  for (const zone of scene.zones.values()) zoneParents.set(zone.id, zone.parent);
  for (const zone of diagram.zones.values()) {
    if (!scene.zones.has(zone.id)) zoneParents.set(zone.id, zone.parent);
  }

  // --- Layout of new nodes ----------------------------------------------------
  const edges = finalEdges(diagram, scene);
  const fresh = [...plans.values()].filter((p) => !p.box);
  if (fresh.length > 0) layout(plans, zoneParents, edges, scene, diagram.direction ?? "right");

  // --- Write nodes --------------------------------------------------------
  for (const plan of plans.values()) {
    if (!plan.changed) continue;
    const box = plan.box as Box;
    if (plan.existing) {
      if (plan.rebuild) rebuildNode(elements, plan, plan.existing, style);
      else retitle(elements, plan.existing, plan.title);
      result.updated++;
    } else {
      for (const el of buildNode(plan, box.x, box.y, style)) elements.set(el.id, el);
      result.created++;
    }
  }

  // --- Zones, sized around what they hold ------------------------------------
  const zoneBoxes = new Map<string, Box>();
  const depthOf = (id: string): number => {
    const parent = zoneParents.get(id);
    return parent ? 1 + depthOf(parent) : 0;
  };
  const zoneIds = [...zoneParents.keys()].sort((a, b) => depthOf(b) - depthOf(a));
  for (const id of zoneIds) {
    const members = [
      ...[...plans.values()].filter((p) => p.zone === id && p.box).map((p) => p.box as Box),
      ...[...zoneParents.entries()].filter(([, parent]) => parent === id).map(([child]) => zoneBoxes.get(child)).filter((b): b is Box => !!b),
    ];
    const inner = unionBox(members);
    const existing = scene.zones.get(id);
    let box: Box;
    if (inner) {
      const padded = { x: inner.x - ZONE_PAD, y: inner.y - ZONE_TITLE_SPACE, width: inner.width + 2 * ZONE_PAD, height: inner.height + ZONE_TITLE_SPACE + ZONE_PAD };
      // Only ever grows: a zone the operator drew larger than its contents was
      // drawn that way on purpose.
      box = existing ? (unionBox([existing.box, padded]) as Box) : padded;
    } else {
      box = existing?.box ?? emptyZoneBox(scene);
    }
    zoneBoxes.set(id, box);

    const declared = diagram.zones.get(id);
    const kind: ZoneKind = declared?.kind ?? existing?.kind ?? "network";
    const title = declared?.title ?? existing?.title ?? id;
    const unchanged =
      existing && kind === existing.kind && title === existing.title && sameBox(box, existing.box);
    if (unchanged) continue;
    if (existing) {
      for (const elId of existing.elementIds) if (elId !== existing.frameId) tombstone(elements, elId);
      result.updated++;
    } else {
      result.created++;
    }
    const zoneStyle = existing?.style ?? style;
    const built = skeletonsToElements(
      buildZone({ kind, title, width: box.width, height: box.height, style: zoneStyle, node: id }),
      { x: box.x, y: box.y, frameId: existing?.frameId, z: ZONE_Z + depthOf(id) },
    );
    writeBuilt(elements, built);
  }

  // --- Connections --------------------------------------------------------------
  const frames = new Map<string, string>();
  for (const plan of plans.values()) frames.set(plan.id, plan.existing?.frameId ?? frameIdOf(elements, plan.id) ?? "");
  const rebuilt = new Set([...plans.values()].filter((p) => p.rebuild && p.existing).map((p) => p.id));

  // Arrows whose geometry is drawn at the end, once every shape is in place.
  const toLayout: string[] = [];

  for (const edge of diagram.edges) {
    const match = findEdge(scene, edge.from, edge.to, edge.arrow, edge.label);
    const implied = defaultHeads(edge.arrow);
    if (match) {
      const arrow = elements.get(match.id);
      if (!arrow) continue;
      const wantStyle = edge.style ?? match.style;
      const wantLabel = edge.label ?? match.label;
      // An existing connection keeps its route unless this text names one,
      // for it or for all of its connections.
      const wantRoute = edge.route ?? diagram.arrows ?? match.route;
      const wantHead = edge.head ?? (edge.arrow === match.arrow ? match.head : implied.head);
      const wantTail = edge.tail ?? (edge.arrow === match.arrow ? match.tail : implied.tail);
      if (
        wantStyle === match.style &&
        wantLabel === match.label &&
        edge.arrow === match.arrow &&
        wantRoute === match.route &&
        wantHead === match.head &&
        wantTail === match.tail
      ) {
        continue;
      }
      const look = edgeLook(wantStyle, style);
      // A read reports a connection drawn end-to-start the right way round;
      // writing its heads back has to put them on the ends they are on.
      const reversed = endOwner(arrow, "startBinding", scene) === edge.to;
      const heads = reversed ? { head: wantTail, tail: wantHead } : { head: wantHead, tail: wantTail };
      elements.set(match.id, {
        ...arrow,
        strokeColor: look.strokeColor,
        strokeStyle: look.strokeStyle,
        customData: look.customData,
        ...excalidrawHeads(heads.head, heads.tail),
        ...routeFields(wantRoute),
        version: (arrow.version ?? 1) + 1,
      });
      setEdgeLabel(elements, match.id, wantLabel, look.strokeColor, look.fontFamily, look.customData.logos);
      if (wantRoute !== match.route) toLayout.push(match.id);
      result.updated++;
      continue;
    }
    const from = frames.get(edge.from);
    const to = frames.get(edge.to);
    if (!from || !to) continue;
    const look = edgeLook(edge.style ?? "plain", style);
    const arrow = normalizeElement({
      type: "arrow",
      strokeColor: look.strokeColor,
      strokeStyle: look.strokeStyle,
      strokeWidth: look.strokeWidth,
      roughness: look.roughness,
      startBinding: from,
      endBinding: to,
      customData: look.customData,
      z: EDGE_Z,
    });
    Object.assign(arrow, excalidrawHeads(edge.head ?? implied.head, edge.tail ?? implied.tail), routeFields(edge.route ?? diagram.arrows ?? "elbow"));
    elements.set(arrow.id, arrow);
    elements.set(arrow.id, placeBoundArrow(elements, arrow));
    wireBindings(elements, [arrow]);
    if (edge.label) setEdgeLabel(elements, arrow.id, edge.label, look.strokeColor, look.fontFamily, look.customData.logos);
    toLayout.push(arrow.id);
    result.connected++;
  }

  // Arrows into a card that was rebuilt start from its old outline; redraw
  // them against the new one.
  for (const edge of scene.edges) {
    if (rebuilt.has(edge.from) || rebuilt.has(edge.to)) toLayout.push(edge.id);
  }

  // Everything is in place: draw the arrows, around every shape but their own
  // two ends. An elbow already on the canvas that a new node now sits on is
  // redrawn too; one that crosses nothing new keeps the route it has, which
  // may be one an operator adjusted by hand.
  const after = readScene(live());
  const obstacles: Obstacle[] = [
    ...[...after.nodes.values()].map((n) => ({ id: n.frameId, box: n.box })),
    ...[...after.zones.values()].map((z) => ({ id: `${z.frameId}:title`, box: zoneTitleBox(z) })),
  ];
  const added = [...plans.values()].filter((p) => !p.existing && p.box).map((p) => p.box as Box);
  for (const edge of after.edges) {
    const arrow = elements.get(edge.id);
    if (!arrow || arrow.elbowed !== true || toLayout.includes(edge.id)) continue;
    const points = arrow.points as [number, number][];
    if (added.some((box) => pathCrosses(points, [Number(arrow.x), Number(arrow.y)], box))) toLayout.push(edge.id);
  }
  layoutArrows(elements, toLayout, obstacles);

  return result;
}

// ---------------------------------------------------------------------------
// Checks

function check(diagram: Diagram, scene: Scene, result: DiagramResult): void {
  const problems: DiagramProblem[] = [];
  const isNode = (id: string) => diagram.nodes.has(id) || scene.nodes.has(id);
  const isZone = (id: string) => diagram.zones.has(id) || scene.zones.has(id);

  for (const node of diagram.nodes.values()) {
    if (!node.kind && !scene.nodes.has(node.id)) {
      problems.push({
        line: node.line,
        message: `"${node.id}" is new, so it needs a kind: ${node.id}: server "${node.title ?? node.id}"`,
      });
    }
    if (scene.zones.has(node.id)) problems.push({ line: node.line, message: `"${node.id}" is a zone on the canvas, not a node` });
  }
  for (const zone of diagram.zones.values()) {
    if (scene.nodes.has(zone.id)) problems.push({ line: zone.line, message: `"${zone.id}" is a node on the canvas, not a zone` });
  }
  for (const edge of diagram.edges) {
    for (const end of [edge.from, edge.to]) {
      if (isNode(end)) continue;
      problems.push({
        line: edge.line,
        message: isZone(end)
          ? `"${end}" is a zone; connect the nodes inside it instead`
          : `no node "${end}". Declare it (${end}: server "${end}") or use an id from the drawing`,
      });
    }
  }
  if (problems.length > 0) throw new DiagramError(problems.slice(0, 20));
  if (diagram.nodes.size + scene.nodes.size > 400) {
    result.warnings.push("this drawing has over 400 nodes; consider splitting it into several drawings");
  }
}

// ---------------------------------------------------------------------------
// Layout

interface LayoutEdge {
  from: string;
  to: string;
}

function finalEdges(diagram: Diagram, scene: Scene): LayoutEdge[] {
  return [
    ...scene.edges.map((e) => ({ from: e.from, to: e.to })),
    ...diagram.edges.map((e) => ({ from: e.from, to: e.to })),
  ];
}

function nodeSize(kind: string, title: string, detail: string, icon?: string): { width: number; height: number } {
  if (asCard(kind, icon)) return { width: cardWidth(title, detail), height: CARD_HEIGHT };
  if (kind === "note") return { width: 220, height: Math.max(130, measureText(title, 16, 200).height + 32) };
  const measured = measureText(title, 16);
  return { width: Math.max(140, Math.ceil(measured.width) + 40), height: Math.max(60, Math.ceil(measured.height) + 28) };
}

/**
 * Give every new node a box.
 *
 * dagre lays out the whole diagram, old nodes and new, so new nodes come out
 * in a sensible place *relative* to the graph. Old nodes then stay exactly
 * where they are on the canvas, and each new node is put at its laid-out
 * offset from the nearest node that already has a place, which keeps it
 * beside what it connects to. Last, anything that would overlap is moved
 * along the cross-axis until it is clear.
 */
function layout(
  plans: Map<string, Plan>,
  zoneParents: Map<string, string | undefined>,
  edges: readonly LayoutEdge[],
  scene: Scene,
  direction: "right" | "down",
): void {
  const g = layoutGraph(plans, zoneParents, edges, direction);
  const laid = (id: string) => g.node(id) as { x: number; y: number };
  const placed = [...plans.values()].filter((p) => p.box);
  const obstacles: Box[] = [
    ...placed.map((p) => p.box as Box),
    ...scene.others.map((o) => ({ x: o.x, y: o.y, width: 1, height: 1 })),
  ];

  // Neighbours in the laid-out graph, both directions, for finding an anchor.
  const neighbours = new Map<string, string[]>();
  for (const e of edges) {
    if (!plans.has(e.from) || !plans.has(e.to)) continue;
    neighbours.set(e.from, [...(neighbours.get(e.from) ?? []), e.to]);
    neighbours.set(e.to, [...(neighbours.get(e.to) ?? []), e.from]);
  }

  // Where unconnected new nodes go: beside everything already drawn.
  const drawn = scene.bounds;
  const looseOrigin =
    placed.length === 0 && !drawn
      ? { x: 0, y: 0 }
      : direction === "right"
        ? { x: (drawn ? drawn.x + drawn.width : 0) + RANK_GAP, y: drawn ? drawn.y : 0 }
        : { x: drawn ? drawn.x : 0, y: (drawn ? drawn.y + drawn.height : 0) + RANK_GAP };
  const fresh = [...plans.values()].filter((p) => !p.box);
  const freshMin = {
    x: Math.min(...fresh.map((p) => laid(p.id).x - p.width / 2)),
    y: Math.min(...fresh.map((p) => laid(p.id).y - p.height / 2)),
  };

  // Place connected nodes first, nearest to what is already placed, so each
  // has an anchor by the time it is reached.
  const queue = [...fresh].sort((a, b) => hops(a.id) - hops(b.id));
  for (const plan of queue) {
    const anchor = nearestPlaced(plan.id);
    const self = laid(plan.id);
    let center: { x: number; y: number };
    if (anchor) {
      const a = laid(anchor.id);
      const ab = anchor.box as Box;
      center = { x: ab.x + ab.width / 2 + (self.x - a.x), y: ab.y + ab.height / 2 + (self.y - a.y) };
    } else if (plan.zone && scene.zones.has(plan.zone)) {
      // Into a zone that is on the canvas but holds nothing placed yet: its
      // top-left corner, below its title.
      const zb = (scene.zones.get(plan.zone) as SceneZone).box;
      center = { x: zb.x + ZONE_PAD + plan.width / 2, y: zb.y + ZONE_TITLE_SPACE + plan.height / 2 };
    } else {
      center = { x: looseOrigin.x + (self.x - freshMin.x), y: looseOrigin.y + (self.y - freshMin.y) };
    }
    let box: Box = { x: center.x - plan.width / 2, y: center.y - plan.height / 2, width: plan.width, height: plan.height };
    box = clear(box, obstacles, direction);
    plan.box = box;
    obstacles.push(box);
  }

  function hops(id: string): number {
    const seen = new Set([id]);
    let frontier = [id];
    for (let depth = 0; frontier.length > 0 && depth < 50; depth++) {
      if (frontier.some((n) => plans.get(n)?.box)) return depth;
      frontier = frontier.flatMap((n) => neighbours.get(n) ?? []).filter((n) => !seen.has(n) && seen.add(n));
    }
    return Infinity;
  }

  function nearestPlaced(id: string): Plan | null {
    const seen = new Set([id]);
    let frontier = [id];
    while (frontier.length > 0) {
      const next: string[] = [];
      for (const n of frontier) {
        for (const m of neighbours.get(n) ?? []) {
          if (seen.has(m)) continue;
          seen.add(m);
          const p = plans.get(m);
          if (p?.box) return p;
          next.push(m);
        }
      }
      frontier = next;
    }
    // Not connected to anything placed: anchor on a placed node in its zone.
    const zone = plans.get(id)?.zone;
    return zone ? ([...plans.values()].find((p) => p.zone === zone && p.box) ?? null) : null;
  }
}

/**
 * Run dagre over the diagram, and never fail.
 *
 * dagre gets one edge per pair of nodes, whichever way and however many
 * connections the pair has, weighted by the count. It only needs to know
 * what is connected, since the arrows themselves are drawn elsewhere
 * (arrow-layout.ts). Repeated connections inside zones made it throw ("Not
 * possible to find intersection inside of the rectangle"), and an
 * interaction diagram is full of them: browser to server and back, three
 * times. If it throws anyway, it is run again without zones, and as a last
 * resort the nodes go in a grid. A layout problem must not fail the write.
 */
function layoutGraph(
  plans: Map<string, Plan>,
  zoneParents: Map<string, string | undefined>,
  edges: readonly LayoutEdge[],
  direction: "right" | "down",
): { node(id: string): { x: number; y: number } } {
  const weights = new Map<string, { from: string; to: string; weight: number }>();
  for (const e of edges) {
    if (e.from === e.to || !plans.has(e.from) || !plans.has(e.to)) continue;
    const key = [e.from, e.to].sort().join("\u0000");
    const prior = weights.get(key);
    if (prior) prior.weight++;
    else weights.set(key, { from: e.from, to: e.to, weight: 1 });
  }

  const run = (withZones: boolean) => {
    const g = new dagre.graphlib.Graph({ compound: withZones });
    g.setGraph({ rankdir: direction === "right" ? "LR" : "TB", nodesep: NODE_GAP, ranksep: RANK_GAP, marginx: 0, marginy: 0 });
    g.setDefaultEdgeLabel(() => ({}));
    if (withZones) {
      for (const zone of zoneParents.keys()) g.setNode(`zone:${zone}`, {});
      for (const [zone, parent] of zoneParents) if (parent) g.setParent(`zone:${zone}`, `zone:${parent}`);
    }
    for (const plan of plans.values()) {
      g.setNode(plan.id, { width: plan.width, height: plan.height });
      if (withZones && plan.zone && zoneParents.has(plan.zone)) g.setParent(plan.id, `zone:${plan.zone}`);
    }
    for (const e of weights.values()) g.setEdge(e.from, e.to, { weight: e.weight });
    dagre.layout(g);
    return { node: (id: string) => g.node(id) as { x: number; y: number } };
  };

  for (const withZones of [true, false]) {
    try {
      return run(withZones);
    } catch (err) {
      console.warn(`diagram layout failed${withZones ? " with zones" : ""}; ${withZones ? "retrying without" : "using a grid"}:`, err);
    }
  }
  // A grid, in the order the nodes were declared.
  const ids = [...plans.keys()];
  const columns = Math.max(1, Math.ceil(Math.sqrt(ids.length)));
  const at = new Map(
    ids.map((id, i) => [id, { x: (i % columns) * (260 + NODE_GAP), y: Math.floor(i / columns) * (CARD_HEIGHT + NODE_GAP) }]),
  );
  return { node: (id: string) => at.get(id) as { x: number; y: number } };
}

/** Move a box along the cross-axis until it overlaps nothing. */
function clear(box: Box, obstacles: readonly Box[], direction: "right" | "down"): Box {
  let out = box;
  for (let i = 0; i < 200; i++) {
    const hit = obstacles.find((o) => overlap(out, o, CLEARANCE));
    if (!hit) return out;
    out =
      direction === "right"
        ? { ...out, y: hit.y + hit.height + CLEARANCE }
        : { ...out, x: hit.x + hit.width + CLEARANCE };
  }
  return out;
}

function overlap(a: Box, b: Box, margin: number): boolean {
  return a.x < b.x + b.width + margin && b.x < a.x + a.width + margin && a.y < b.y + b.height + margin && b.y < a.y + a.height + margin;
}

function sameBox(a: Box, b: Box): boolean {
  return Math.abs(a.x - b.x) < 1 && Math.abs(a.y - b.y) < 1 && Math.abs(a.width - b.width) < 1 && Math.abs(a.height - b.height) < 1;
}

function emptyZoneBox(scene: Scene): Box {
  const drawn = scene.bounds;
  return { x: drawn ? drawn.x + drawn.width + RANK_GAP : 0, y: drawn ? drawn.y : 0, width: 460, height: 280 };
}

// ---------------------------------------------------------------------------
// Elements

function tombstone(elements: Y.Map<NormalizedElement>, id: string): void {
  const el = elements.get(id);
  if (!el || el.isDeleted) return;
  elements.set(id, { ...el, isDeleted: true, version: (el.version ?? 1) + 1, updated: Date.now() });
}

/** Write freshly built elements. One that reuses an existing id (a rebuilt
 * frame) must outrank the copy every canvas holds, or no canvas takes it. */
function writeBuilt(elements: Y.Map<NormalizedElement>, built: readonly NormalizedElement[]): void {
  for (const el of built) {
    const prior = elements.get(el.id);
    if (prior) {
      const arrows = ((prior.boundElements as { id: string; type: string }[] | null) ?? []).filter((b) => b.type === "arrow");
      const own = (el.boundElements as { id: string; type: string }[] | null) ?? [];
      elements.set(el.id, {
        ...el,
        boundElements: [...own, ...arrows],
        version: Math.max(prior.version ?? 1, el.version) + 1,
        isDeleted: false,
      });
    } else {
      elements.set(el.id, el);
    }
  }
}

function buildNode(plan: Plan, x: number, y: number, style: ShapeStyle, frameId?: string): NormalizedElement[] {
  if (asCard(plan.kind, plan.icon)) {
    const card = buildCard({
      kind: plan.kind,
      title: plan.title,
      detail: plan.detail,
      style,
      badges: plan.badges,
      node: plan.id,
      color: plan.color,
      icon: plan.icon,
    });
    return skeletonsToElements(card.elements, { x, y, frameId, z: NODE_Z });
  }
  return skeletonsToElements(genericShape(plan, style), { x, y, frameId, z: NODE_Z });
}

/** A plain labelled shape or a note, for what the catalogue has no card for. */
function genericShape(plan: Plan, style: ShapeStyle): Skeleton[] {
  const tag: ShapeTag = {
    shape: plan.kind === "note" ? "note" : `shape:${plan.kind}`,
    part: "frame",
    style,
    node: plan.id,
    ...(plan.color ? { color: plan.color } : {}),
  };
  const clean = style === "clean";
  const note = plan.kind === "note";
  const colors = plan.color ? plainShapeColors(plan.color) : null;
  return [
    {
      type: note ? "rectangle" : (plan.kind === "box" ? "rectangle" : plan.kind) as Skeleton["type"],
      x: 0,
      y: 0,
      width: plan.width,
      height: plan.height,
      backgroundColor: note ? "#fff3bf" : (colors?.backgroundColor ?? "transparent"),
      fillStyle: "solid",
      strokeColor: note ? "#f08c00" : (colors?.strokeColor ?? "#1e1e1e"),
      strokeWidth: note ? 1 : clean ? 1.5 : 2,
      roughness: clean ? 0 : 1,
      roundness: plan.kind === "box" ? { type: 3 } : null,
      customData: { logos: tag },
      label: {
        text: plan.title,
        ...(colors ? { strokeColor: note ? undefined : colors.textColor } : {}),
        fontSize: 16,
        fontFamily: clean ? 6 : 5,
        ...(note ? { textAlign: "left", verticalAlign: "top" } : {}),
        customData: { logos: { ...tag, part: "label" } },
      },
    },
  ];
}

/** Rebuild a node at its current position, keeping its frame's id so every
 * arrow bound to it stays bound. */
function rebuildNode(elements: Y.Map<NormalizedElement>, plan: Plan, existing: SceneNode, style: ShapeStyle): void {
  for (const id of existing.elementIds) if (id !== existing.frameId) tombstone(elements, id);
  // An arrow a person bound to some other part of the card (its title, its
  // icon) would lose its end with that part; bind it to the frame instead.
  const parts = new Set(existing.elementIds);
  for (const el of elements.values()) {
    if (el.isDeleted || (el.type !== "arrow" && el.type !== "line")) continue;
    let next = el;
    for (const end of ["startBinding", "endBinding"] as const) {
      const binding = next[end] as { elementId?: string } | null;
      if (binding?.elementId && binding.elementId !== existing.frameId && parts.has(binding.elementId)) {
        next = { ...next, [end]: { ...binding, elementId: existing.frameId } };
      }
    }
    if (next !== el) elements.set(el.id, { ...next, version: (el.version ?? 1) + 1 });
  }
  const box = existing.box;
  const built = buildNode(plan, box.x, box.y, existing.builtin ? existing.style : style, existing.frameId);
  writeBuilt(elements, built);
  plan.box = { x: box.x, y: box.y, width: plan.width, height: plan.height };
}

/** Change the words of a hand-drawn labelled shape, and nothing else about
 * it: its author chose its size and look. */
function retitle(elements: Y.Map<NormalizedElement>, existing: SceneNode, title: string): void {
  for (const id of existing.elementIds) {
    const el = elements.get(id);
    if (el?.type !== "text") continue;
    const container = typeof el.containerId === "string" ? elements.get(el.containerId) : undefined;
    const fields = boundTextFields(title, Number(el.fontSize) || 16, Number(container?.width) || 100);
    elements.set(id, { ...el, text: fields.text, originalText: fields.originalText, height: fields.height, version: (el.version ?? 1) + 1 });
  }
}

function frameIdOf(elements: Y.Map<NormalizedElement>, nodeId: string): string | undefined {
  for (const el of elements.values()) {
    if (el.isDeleted) continue;
    const logos = (el.customData as { logos?: ShapeTag } | undefined)?.logos;
    if (logos?.node === nodeId && logos.part === "frame") return el.id;
  }
  return undefined;
}

/** Arrowheads in Excalidraw's names; the language's "none" is null. */
function excalidrawHeads(head: string, tail: string): { startArrowhead: string | null; endArrowhead: string | null } {
  const name = (h: string) => (h === "none" ? null : h.replace(/-/g, "_"));
  return { startArrowhead: name(tail), endArrowhead: name(head) };
}

/** The fields that make an arrow elbow, curved or straight. */
function routeFields(route: Route): Record<string, unknown> {
  switch (route) {
    case "elbow":
      return { elbowed: true, roundness: null };
    case "curved":
      return { elbowed: false, roundness: { type: 2 }, fixedSegments: null };
    case "straight":
      return { elbowed: false, roundness: null, fixedSegments: null };
  }
}

/** Which node an arrow's end is attached to. */
function endOwner(arrow: NormalizedElement, end: "startBinding" | "endBinding", scene: Scene): string | undefined {
  const id = (arrow[end] as { elementId?: string } | null)?.elementId;
  return id ? scene.owner.get(id) : undefined;
}

/** The corner of a zone its title sits in, which arrows should not cross. */
function zoneTitleBox(zone: SceneZone): Box {
  return { x: zone.box.x + 8, y: zone.box.y + 4, width: Math.min(zone.box.width - 16, 60 + zone.title.length * 9), height: 32 };
}

function findEdge(scene: Scene, from: string, to: string, arrow: ArrowKind, label?: string) {
  const candidates = scene.edges.filter(
    (e) => (e.from === from && e.to === to) || (arrow !== "->" && e.from === to && e.to === from) || (e.arrow !== "->" && e.from === to && e.to === from),
  );
  return candidates.find((e) => label !== undefined && e.label === label) ?? candidates[0];
}

/** Put a label on an arrow, or change the one it has. */
function setEdgeLabel(
  elements: Y.Map<NormalizedElement>,
  arrowId: string,
  text: string,
  color: string,
  fontFamily: number,
  tag: ShapeTag,
): void {
  const arrow = elements.get(arrowId);
  if (!arrow) return;
  const bound = ((arrow.boundElements as { id: string; type: string }[] | null) ?? []).find((b) => b.type === "text");
  const current = bound ? elements.get(bound.id) : undefined;
  if (current && !current.isDeleted) {
    if (!text) {
      tombstone(elements, current.id);
      return;
    }
    const fields = boundTextFields(text, 16, EDGE_LABEL_WIDTH);
    elements.set(current.id, { ...current, ...fields, strokeColor: color, version: (current.version ?? 1) + 1 });
    placeEdgeLabel(elements, arrowId);
    return;
  }
  if (!text) return;
  const fields = boundTextFields(text, 16, EDGE_LABEL_WIDTH);
  const label = normalizeElement({
    type: "text",
    text: fields.text,
    originalText: fields.originalText,
    fontSize: 16,
    fontFamily,
    strokeColor: color,
    textAlign: "center",
    verticalAlign: "middle",
    containerId: arrowId,
    width: fields.width,
    height: fields.height,
    customData: { logos: { ...tag, part: "label" } },
    // Over its own arrow, still under the cards.
    z: EDGE_Z + 0.5,
  });
  elements.set(label.id, label);
  elements.set(arrowId, {
    ...arrow,
    boundElements: [...((arrow.boundElements as unknown[]) ?? []), { id: label.id, type: "text" }],
    version: (arrow.version ?? 1) + 1,
  });
  placeEdgeLabel(elements, arrowId);
}

/** Centre an arrow's label on the middle of its stroke. Excalidraw does the
 * same when a person moves the arrow; a fresh write has to start there. */
function placeEdgeLabel(elements: Y.Map<NormalizedElement>, arrowId: string): void {
  const arrow = elements.get(arrowId);
  const points = arrow?.points as [number, number][] | undefined;
  if (!arrow || !Array.isArray(points) || points.length < 2) return;
  const bound = ((arrow.boundElements as { id: string; type: string }[] | null) ?? []).find((b) => b.type === "text");
  const label = bound ? elements.get(bound.id) : undefined;
  if (!label || label.isDeleted) return;
  const mid = points[Math.floor(points.length / 2) - (points.length % 2 === 0 ? 1 : 0)];
  const end = points[Math.floor(points.length / 2)];
  const cx = Number(arrow.x) + (mid[0] + end[0]) / 2;
  const cy = Number(arrow.y) + (mid[1] + end[1]) / 2;
  elements.set(label.id, {
    ...label,
    x: cx - Number(label.width) / 2,
    y: cy - Number(label.height) / 2,
    version: (label.version ?? 1) + 1,
  });
}

export type { EdgeStyle, SceneZone };

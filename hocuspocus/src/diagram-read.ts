// Read a drawing as a diagram: nodes, zones and connections, not shapes.
//
// A card on the canvas is a dozen elements (a frame, a tile, the icon's
// strokes, two texts, its badges). Listing those to an agent hides the
// diagram under its geometry: a twenty-host map reads as two hundred lines of
// coordinates. This groups the elements back into what they mean and writes
// them in the diagram language (diagram-language.ts), which is also what the
// agent writes to change the drawing.
//
// What counts as what:
//
//   - a built-in shape is recognised by the tag its parts carry in
//     customData (drawing-catalogue.ts): a card is a node, a zone is a zone,
//     a badge on a card is that card's badge;
//   - a shape somebody drew by hand with a label in it is a node too, of kind
//     box, ellipse or diamond, named by its element id;
//   - an arrow with both ends attached to nodes is a connection;
//   - everything else (free text, freehand strokes, loose arrows, images) is
//     listed apart as "other", so nothing on the canvas goes unmentioned.
//
// A node keeps the id an agent gave it ("dc01"), carried in its tag. A shape
// placed from the panel or drawn by hand has none, and its element id stands
// in, so every node has an id a write can refer to.

import {
  CARD_HEIGHT,
  ZONE_KINDS,
  parseRef,
  tagOf,
  type CardRef,
  type EdgeStyle,
  type ShapeStyle,
  type ShapeTag,
  type ZoneKind,
} from "./drawing-catalogue.js";
import { EDGE_STYLES } from "./drawing-catalogue.js";
import {
  defaultHeads,
  writeDiagram,
  type ArrowKind,
  type Route,
  type Stroke,
  type SerializedEdge,
  type SerializedNode,
  type SerializedZone,
} from "./diagram-language.js";
import type { NormalizedElement } from "./drawing-elements.js";

export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface SceneNode {
  id: string;
  /** A card kind, or box, ellipse, diamond, note. */
  kind: string;
  title: string;
  detail: string;
  badges: string[];
  /** A colour or icon an agent chose over the kind's own. */
  color?: string;
  icon?: string;
  /** The Logos record a card stands for. */
  ref?: CardRef;
  /** The element arrows bind to: a card's frame, a box itself. */
  frameId: string;
  elementIds: string[];
  box: Box;
  style: ShapeStyle;
  /** Built from the catalogue, so it can be rebuilt. */
  builtin: boolean;
  zone?: string;
}

export interface SceneZone {
  id: string;
  kind: ZoneKind;
  title: string;
  frameId: string;
  elementIds: string[];
  box: Box;
  style: ShapeStyle;
  parent?: string;
}

export interface SceneEdge {
  /** The arrow's element id. */
  id: string;
  from: string;
  to: string;
  arrow: ArrowKind;
  label: string;
  labelId?: string;
  style: EdgeStyle;
  route: Route;
  /** Arrowheads in the language's names ("none" for none), after any
   * swap that made `from` the start. */
  head: string;
  tail: string;
  stroke?: Stroke;
}

export interface SceneOther {
  id: string;
  type: string;
  text?: string;
  x: number;
  y: number;
}

/** A sequence diagram on the canvas (sequence-layout.ts). */
export interface SceneSequence {
  id: string;
  /** sequence, timeline or gantt. */
  type: string;
  elementIds: string[];
  box: Box;
  /** The Mermaid it was drawn from. */
  source: string;
  /** Where it was drawn, so a redraw replaces it in place. */
  origin?: { x: number; y: number };
}

export interface Scene {
  nodes: Map<string, SceneNode>;
  sequences: Map<string, SceneSequence>;
  zones: Map<string, SceneZone>;
  edges: SceneEdge[];
  others: SceneOther[];
  /** Element id to the node or zone it belongs to. */
  owner: Map<string, string>;
  /** The box around everything on the canvas, or null when it is empty. */
  bounds: Box | null;
}

function boxOf(el: NormalizedElement): Box {
  const x = Number(el.x) || 0;
  const y = Number(el.y) || 0;
  const points = el.points as [number, number][] | undefined;
  if (Array.isArray(points) && points.length > 0) {
    const xs = points.map((p) => x + p[0]);
    const ys = points.map((p) => y + p[1]);
    return { x: Math.min(...xs), y: Math.min(...ys), width: Math.max(...xs) - Math.min(...xs), height: Math.max(...ys) - Math.min(...ys) };
  }
  return { x, y, width: Number(el.width) || 0, height: Number(el.height) || 0 };
}

export function unionBox(boxes: readonly Box[]): Box | null {
  if (boxes.length === 0) return null;
  const x = Math.min(...boxes.map((b) => b.x));
  const y = Math.min(...boxes.map((b) => b.y));
  const right = Math.max(...boxes.map((b) => b.x + b.width));
  const bottom = Math.max(...boxes.map((b) => b.y + b.height));
  return { x, y, width: right - x, height: bottom - y };
}

function contains(outer: Box, inner: Box): boolean {
  const cx = inner.x + inner.width / 2;
  const cy = inner.y + inner.height / 2;
  return cx > outer.x && cx < outer.x + outer.width && cy > outer.y && cy < outer.y + outer.height;
}

const area = (b: Box) => b.width * b.height;

/** The group a shape's parts share: the outermost one, so a card the operator
 * grouped with something else still reads as that card. */
function unitKey(el: NormalizedElement): string | null {
  const groups = el.groupIds as string[] | undefined;
  return Array.isArray(groups) && groups.length > 0 ? `g:${groups[groups.length - 1]}` : null;
}

export function readScene(live: readonly NormalizedElement[]): Scene {
  const byId = new Map(live.map((el) => [el.id, el]));
  const scene: Scene = { nodes: new Map(), sequences: new Map(), zones: new Map(), edges: [], others: [], owner: new Map(), bounds: null };
  scene.bounds = unionBox(live.map(boxOf));

  // Sequence diagrams first: their parts are read as the diagram they belong
  // to, never as nodes or loose shapes. A label belongs with its container.
  const seqOf = (el: NormalizedElement) => {
    const own = tagOf(el)?.seq;
    if (own) return own;
    const container = typeof el.containerId === "string" ? byId.get(el.containerId) : undefined;
    return container ? tagOf(container)?.seq : undefined;
  };
  const rest: NormalizedElement[] = [];
  for (const el of live) {
    const seq = seqOf(el);
    if (!seq) {
      rest.push(el);
      continue;
    }
    let entry = scene.sequences.get(seq);
    if (!entry) {
      entry = { id: seq, type: "sequence", elementIds: [], box: boxOf(el), source: "" };
      scene.sequences.set(seq, entry);
    }
    entry.elementIds.push(el.id);
    entry.box = unionBox([entry.box, boxOf(el)]) as Box;
    const source = tagOf(el)?.source;
    if (source && !entry.source) entry.source = source;
    const shape = tagOf(el)?.shape ?? "";
    if (shape.endsWith("-part")) entry.type = shape.slice(0, -5);
    const origin = (tagOf(el) as { origin?: unknown } | null)?.origin;
    if (!entry.origin && Array.isArray(origin) && origin.length === 2) entry.origin = { x: Number(origin[0]), y: Number(origin[1]) };
    scene.owner.set(el.id, `sequence:${seq}`);
  }

  // Group the elements into units: a built-in shape's parts, a shape and its
  // label, or a lone element.
  const units = new Map<string, NormalizedElement[]>();
  for (const el of rest) {
    const container = typeof el.containerId === "string" ? byId.get(el.containerId) : undefined;
    const key = (container ? unitKey(container) ?? `e:${container.id}` : null) ?? unitKey(el) ?? `e:${el.id}`;
    if (!units.has(key)) units.set(key, []);
    (units.get(key) as NormalizedElement[]).push(el);
  }

  const arrows: NormalizedElement[] = [];
  const looseBadges: { key: string; box: Box; ids: string[] }[] = [];

  for (const elements of units.values()) {
    const frames = elements.filter((el) => tagOf(el)?.part === "frame");
    const tag = frames.length > 0 ? (tagOf(frames[0]) as ShapeTag) : null;
    const textOf = (partName: string) =>
      wordsOf(elements.find((el) => tagOf(el)?.part === partName && el.type === "text"));

    // A card.
    if (tag?.shape.startsWith("card:")) {
      const frame = frames.find((el) => el.type === "rectangle") ?? frames[0];
      const badges = [
        ...new Set(
          elements
            .map((el) => tagOf(el)?.part ?? "")
            .filter((p) => p.startsWith("badge:"))
            .map((p) => p.slice(6)),
        ),
      ];
      addNode({
        id: tag.node ?? frame.id,
        kind: tag.shape.slice(5),
        title: textOf("title"),
        detail: textOf("detail"),
        badges,
        frameId: frame.id,
        elementIds: elements.map((el) => el.id),
        box: boxOf(frame),
        style: tag.style ?? "sketch",
        builtin: true,
        ...(tag.color ? { color: tag.color } : {}),
        ...(tag.icon ? { icon: tag.icon } : {}),
        ...(parseRef(tag.ref) ? { ref: parseRef(tag.ref) as CardRef } : {}),
      });
      continue;
    }

    // A zone.
    if (tag?.shape.startsWith("zone:") && tag.shape.slice(5) in ZONE_KINDS) {
      const frame = frames.find((el) => el.type === "rectangle") ?? frames[0];
      const id = uniqueId(tag.node ?? frame.id, frame.id);
      scene.zones.set(id, {
        id,
        kind: tag.shape.slice(5) as ZoneKind,
        title: textOf("title"),
        frameId: frame.id,
        elementIds: elements.map((el) => el.id),
        box: boxOf(frame),
        style: tag.style ?? "sketch",
      });
      for (const el of elements) scene.owner.set(el.id, id);
      continue;
    }

    // A badge placed from the panel on its own: it belongs to the card it
    // sits on, worked out once every card is known.
    if (tag?.shape.startsWith("badge:")) {
      looseBadges.push({ key: tag.shape.slice(6), box: boxOf(frames[0]), ids: elements.map((el) => el.id) });
      continue;
    }

    // A note, or a plain shape the diagram language drew: a node whose title
    // is its text. A state diagram's start and end markers have none.
    if (tag?.shape === "note" || tag?.shape.startsWith("shape:")) {
      const frame = frames[0];
      const label = elements.find((el) => el.type === "text");
      addNode({
        id: tag.node ?? frame.id,
        kind: tag.shape === "note" ? "note" : tag.shape.slice(6),
        title: wordsOf(label),
        detail: "",
        badges: [
          ...new Set(
            elements
              .map((el) => tagOf(el)?.part ?? "")
              .filter((p) => p.startsWith("badge:"))
              .map((p) => p.slice(6)),
          ),
        ],
        frameId: frame.id,
        elementIds: elements.map((el) => el.id),
        box: boxOf(frame),
        style: tag.style ?? "sketch",
        builtin: true,
        ...(tag.color ? { color: tag.color } : {}),
      });
      continue;
    }

    // An arrow, built-in or hand-drawn: a connection once both ends are known.
    const arrow = elements.find((el) => el.type === "arrow" || el.type === "line");
    if (arrow && elements.every((el) => el === arrow || el.containerId === arrow.id)) {
      arrows.push(arrow);
      continue;
    }

    // A hand-drawn shape with a label in it.
    const shape = elements.find((el) => ["rectangle", "ellipse", "diamond"].includes(el.type));
    const label = elements.find((el) => el.type === "text" && el.containerId === shape?.id);
    if (!tag && shape && label && elements.length === 2) {
      addNode({
        id: shape.id,
        kind: shape.type === "rectangle" ? "box" : shape.type,
        title: wordsOf(label),
        detail: "",
        badges: [],
        frameId: shape.id,
        elementIds: [shape.id, label.id],
        box: boxOf(shape),
        style: Number(shape.roughness) === 0 ? "clean" : "sketch",
        builtin: false,
      });
      continue;
    }

    for (const el of elements) addOther(el);
  }

  // Badges dropped onto cards from the panel.
  for (const badge of looseBadges) {
    const card = [...scene.nodes.values()].find((n) => n.builtin && n.kind !== "note" && overlaps(n.box, badge.box));
    if (card) {
      if (!card.badges.includes(badge.key)) card.badges.push(badge.key);
      for (const id of badge.ids) scene.owner.set(id, card.id);
      card.elementIds.push(...badge.ids);
    } else {
      scene.others.push({ id: badge.ids[0], type: `badge ${badge.key}`, x: Math.round(badge.box.x), y: Math.round(badge.box.y) });
    }
  }

  // Connections.
  for (const arrow of arrows) {
    const ownerOf = (end: "startBinding" | "endBinding") => {
      const id = (arrow[end] as { elementId?: string } | null)?.elementId;
      const owner = id ? scene.owner.get(id) : undefined;
      return owner && (scene.nodes.has(owner) || scene.zones.has(owner)) ? owner : undefined;
    };
    let from = ownerOf("startBinding");
    let to = ownerOf("endBinding");
    const label = live.find((el) => el.type === "text" && el.containerId === arrow.id);
    // A shape connected to itself is a loop, and a connection like any other.
    if (!from || !to) {
      addOther(arrow);
      if (label) addOther(label);
      continue;
    }
    const headName = (v: unknown) => (typeof v === "string" && v !== "" ? v.replace(/_/g, "-") : "none");
    let head = headName(arrow.endArrowhead);
    let tail = headName(arrow.startArrowhead);
    const startHead = tail !== "none";
    const endHead = head !== "none";
    const kind: ArrowKind = startHead && endHead ? "<->" : endHead || startHead ? "->" : "--";
    if (startHead && !endHead) {
      [from, to] = [to, from];
      [head, tail] = [tail, head];
    }
    const styleTag = tagOf(arrow)?.shape;
    const style = styleTag?.startsWith("edge:") && styleTag.slice(5) in EDGE_STYLES ? (styleTag.slice(5) as EdgeStyle) : "plain";
    const route: Route = arrow.elbowed === true ? "elbow" : arrow.roundness ? "curved" : "straight";
    // A line drawn thicker than its style's, or dashed where the style is
    // solid, was asked for as thick or dotted.
    const stroke: Stroke | undefined =
      Number(arrow.strokeWidth) >= 4 ? "thick" : arrow.strokeStyle !== EDGE_STYLES[style].dash && arrow.strokeStyle !== "solid" ? "dotted" : undefined;
    scene.edges.push({ id: arrow.id, from, to, arrow: kind, label: wordsOf(label), labelId: label?.id, style, route, head, tail, ...(stroke ? { stroke } : {}) });
    scene.owner.set(arrow.id, arrow.id);
    if (label) scene.owner.set(label.id, arrow.id);
  }

  // Which zone each node and zone sits in: the smallest one holding its centre.
  const zones = [...scene.zones.values()];
  const innermost = (box: Box, self?: string) =>
    zones
      .filter((z) => z.id !== self && contains(z.box, box) && area(z.box) > area(box))
      .sort((a, b) => area(a.box) - area(b.box))[0]?.id;
  for (const zone of zones) zone.parent = innermost(zone.box, zone.id);
  for (const node of scene.nodes.values()) node.zone = innermost(node.box);

  return scene;

  function uniqueId(preferred: string, fallback: string): string {
    // A copied card carries its original's id in its tag. The first keeps it;
    // the copy answers to its own element id.
    return scene.nodes.has(preferred) || scene.zones.has(preferred) ? fallback : preferred;
  }

  function addNode(node: SceneNode) {
    node.id = uniqueId(node.id, node.frameId);
    scene.nodes.set(node.id, node);
    for (const id of node.elementIds) scene.owner.set(id, node.id);
  }

  function addOther(el: NormalizedElement) {
    scene.others.push({
      id: el.id,
      type: el.type,
      ...(wordsOf(el) !== "" ? { text: wordsOf(el) } : {}),
      x: Math.round(Number(el.x) || 0),
      y: Math.round(Number(el.y) || 0),
    });
  }
}

/** A text element's words as written. A label is stored wrapped to its
 * shape, with the unwrapped words in originalText; reading the wrapped copy
 * would report line breaks nobody typed, and re-sending a read unchanged would
 * look like an edit. */
function wordsOf(el: NormalizedElement | undefined): string {
  if (!el) return "";
  const words = typeof el.originalText === "string" && el.originalText !== "" ? el.originalText : el.text;
  return typeof words === "string" ? words : "";
}

function overlaps(a: Box, b: Box): boolean {
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

/** Reading order: rows top to bottom, left to right within a row. */
function readingOrder(a: { box: Box }, b: { box: Box }): number {
  const row = (box: Box) => Math.round((box.y + box.height / 2) / CARD_HEIGHT);
  return row(a.box) - row(b.box) || a.box.x - b.box.x;
}

/** The scene as diagram text. */
export function sceneToText(scene: Scene): string {
  const nodes = [...scene.nodes.values()].sort(readingOrder);
  const serial = (n: SceneNode): SerializedNode => ({
    id: n.id,
    kind: n.kind,
    title: n.title,
    detail: n.detail,
    badges: n.badges,
    color: n.color,
    icon: n.icon,
    ref: n.ref,
  });
  const zoneOut = (zone: SceneZone): SerializedZone => ({
    id: zone.id,
    kind: zone.kind,
    title: zone.title,
    zones: [...scene.zones.values()].filter((z) => z.parent === zone.id).sort(readingOrder).map(zoneOut),
    nodes: nodes.filter((n) => n.zone === zone.id).map(serial),
  });
  const allClean = nodes.length > 0 && nodes.every((n) => n.style === "clean");
  // The route most connections use goes in an `arrows` line; only the others
  // say theirs. Without it a drawing of straight arrows would repeat
  // [straight] on every line.
  const counts = new Map<Route, number>();
  for (const e of scene.edges) counts.set(e.route, (counts.get(e.route) ?? 0) + 1);
  const usual: Route = [...counts.entries()].sort((a, b) => b[1] - a[1] || (a[0] === "elbow" ? -1 : 1))[0]?.[0] ?? "elbow";
  const edges: SerializedEdge[] = scene.edges.map((e) => {
    const implied = defaultHeads(e.arrow);
    return {
      from: e.from,
      to: e.to,
      arrow: e.arrow,
      label: e.label,
      style: e.style,
      ...(e.route !== usual ? { route: e.route } : {}),
      ...(e.head !== implied.head ? { head: e.head } : {}),
      ...(e.tail !== implied.tail ? { tail: e.tail } : {}),
      ...(e.stroke ? { stroke: e.stroke } : {}),
    };
  });
  return writeDiagram({
    style: allClean ? "clean" : undefined,
    arrows: usual,
    zones: [...scene.zones.values()].filter((z) => !z.parent).sort(readingOrder).map(zoneOut),
    nodes: nodes.filter((n) => !n.zone).map(serial),
    edges,
  });
}

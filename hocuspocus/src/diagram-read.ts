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

import { CARD_HEIGHT, ZONE_KINDS, tagOf, type EdgeStyle, type ShapeStyle, type ShapeTag, type ZoneKind } from "./drawing-catalogue.js";
import { EDGE_STYLES } from "./drawing-catalogue.js";
import {
  writeDiagram,
  type ArrowKind,
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
}

export interface SceneOther {
  id: string;
  type: string;
  text?: string;
  x: number;
  y: number;
}

export interface Scene {
  nodes: Map<string, SceneNode>;
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
  const scene: Scene = { nodes: new Map(), zones: new Map(), edges: [], others: [], owner: new Map(), bounds: null };
  scene.bounds = unionBox(live.map(boxOf));

  // Group the elements into units: a built-in shape's parts, a shape and its
  // label, or a lone element.
  const units = new Map<string, NormalizedElement[]>();
  for (const el of live) {
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
      String(elements.find((el) => tagOf(el)?.part === partName && el.type === "text")?.text ?? "");

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
    // is its text.
    if (tag?.shape === "note" || tag?.shape.startsWith("shape:")) {
      const frame = frames[0];
      const label = elements.find((el) => el.type === "text");
      addNode({
        id: tag.node ?? frame.id,
        kind: tag.shape === "note" ? "note" : tag.shape.slice(6),
        title: String(label?.text ?? ""),
        detail: "",
        badges: [],
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
        title: String(label.text ?? ""),
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
      return owner && scene.nodes.has(owner) ? owner : undefined;
    };
    let from = ownerOf("startBinding");
    let to = ownerOf("endBinding");
    const label = live.find((el) => el.type === "text" && el.containerId === arrow.id);
    if (!from || !to || from === to) {
      addOther(arrow);
      if (label) addOther(label);
      continue;
    }
    const startHead = arrow.startArrowhead != null;
    const endHead = arrow.endArrowhead != null;
    let kind: ArrowKind = startHead && endHead ? "<->" : endHead ? "->" : startHead ? "->" : "--";
    if (startHead && !endHead) [from, to] = [to, from];
    if (arrow.type === "line" && !startHead && !endHead) kind = "--";
    const styleTag = tagOf(arrow)?.shape;
    const style = styleTag?.startsWith("edge:") && styleTag.slice(5) in EDGE_STYLES ? (styleTag.slice(5) as EdgeStyle) : "plain";
    scene.edges.push({ id: arrow.id, from, to, arrow: kind, label: String(label?.text ?? ""), labelId: label?.id, style });
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
      ...(typeof el.text === "string" && el.text !== "" ? { text: el.text } : {}),
      x: Math.round(Number(el.x) || 0),
      y: Math.round(Number(el.y) || 0),
    });
  }
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
  });
  const zoneOut = (zone: SceneZone): SerializedZone => ({
    id: zone.id,
    kind: zone.kind,
    title: zone.title,
    zones: [...scene.zones.values()].filter((z) => z.parent === zone.id).sort(readingOrder).map(zoneOut),
    nodes: nodes.filter((n) => n.zone === zone.id).map(serial),
  });
  const allClean = nodes.length > 0 && nodes.every((n) => n.style === "clean");
  const edges: SerializedEdge[] = scene.edges.map((e) => ({ from: e.from, to: e.to, arrow: e.arrow, label: e.label, style: e.style }));
  return writeDiagram({
    style: allClean ? "clean" : undefined,
    zones: [...scene.zones.values()].filter((z) => !z.parent).sort(readingOrder).map(zoneOut),
    nodes: nodes.filter((n) => !n.zone).map(serial),
    edges,
  });
}

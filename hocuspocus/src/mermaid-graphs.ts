// Mermaid state diagrams and mindmaps, as graphs.
//
// Both are nodes joined by lines, so both become the same diagram a
// flowchart does (mermaid-flowchart.ts) and are drawn by the same code:
// cards, zones, layout, routed arrows, merging by id. They read back as a
// flowchart, which is what they are once drawn.
//
//   state diagram    a state is a box (or a card, by class); a composite
//                    state is a zone; [*] is a start or end marker; a note
//                    is a note, attached by a dotted line; a transition's
//                    words are its label.
//   mindmap          a tree, root on the left; each first branch and its
//                    whole subtree take one colour; ::icon(x) makes a node a
//                    card with that icon, when it is one of ours.

import {
  BADGE_KEYS,
  CARD_KINDS,
  COLOR_NAMES,
  ICON_NAMES,
  ZONE_KINDS,
  type ZoneKind,
} from "./drawing-catalogue.js";
import { GENERIC_KINDS, type Diagram, type DiagramNode } from "./diagram-language.js";
import { commentExtras, idFrom, lineOf, plain } from "./mermaid-common.js";

const KINDS: ReadonlySet<string> = new Set([...CARD_KINDS, ...GENERIC_KINDS]);

/** Our vocabulary out of a class list: kind, badges, colour. The rest is
 * reported, not failed on. */
function applyClasses(node: DiagramNode, classes: string[], warnings: string[], what: string): void {
  for (const c of classes) {
    const k = c.toLowerCase();
    if (KINDS.has(k)) node.kind = k;
    else if (BADGE_KEYS.includes(k)) node.addBadges.push(k);
    else if (COLOR_NAMES.includes(k)) node.color = k;
    else warnings.push(`class "${c}" on ${what} means nothing here`);
  }
}

function merge(extras: Diagram, node: DiagramNode): DiagramNode {
  const deco = extras.nodes.get(node.id);
  if (!deco) return node;
  return {
    ...node,
    ...(deco.kind ? { kind: deco.kind } : {}),
    ...(deco.title !== undefined ? { title: deco.title } : {}),
    ...(deco.detail !== undefined ? { detail: deco.detail } : {}),
    ...(deco.color !== undefined ? { color: deco.color } : {}),
    ...(deco.icon !== undefined ? { icon: deco.icon } : {}),
    addBadges: [...node.addBadges, ...deco.addBadges],
    removeBadges: deco.removeBadges,
  };
}

interface StateNode {
  id: string;
  label?: unknown;
  shape?: string;
  parentId?: string;
  isGroup?: boolean;
  cssClasses?: string;
}
interface StateDb {
  getData(): { nodes: StateNode[]; edges: { start: string; end: string; label?: unknown }[] };
  getDirection?(): string;
}

export function stateToDiagram(source: string, db: unknown): { diagram: Diagram; warnings: string[] } {
  const extras = commentExtras(source);
  const data = (db as StateDb).getData();
  const warnings: string[] = [];
  const diagram: Diagram = { ...extras, nodes: new Map(), zones: new Map(), edges: [...extras.edges] };
  const direction = (db as StateDb).getDirection?.() ?? "TB";
  diagram.direction ??= direction === "LR" || direction === "RL" ? "right" : "down";

  const byId = new Map(data.nodes.map((n) => [n.id, n]));
  const taken = new Set<string>();
  // Mermaid's ids for notes ("Recon----note-6") are not ids anyone wrote.
  const idOf = new Map<string, string>();
  const ourId = (id: string) => {
    if (!idOf.has(id)) idOf.set(id, /^[A-Za-z_][A-Za-z0-9_]*$/.test(id) ? (taken.add(id), id) : idFrom(id, taken));
    return idOf.get(id) as string;
  };
  const zoneOf = (n: StateNode): string | undefined => {
    let parent = n.parentId ? byId.get(n.parentId) : undefined;
    // A note sits in a group of its own; its zone is that group's.
    while (parent && parent.shape === "noteGroup") parent = parent.parentId ? byId.get(parent.parentId) : undefined;
    return parent && parent.isGroup ? ourId(parent.id) : undefined;
  };
  const classesOf = (n: StateNode) =>
    (n.cssClasses ?? "").split(/\s+/).filter((c) => c && !c.startsWith("statediagram"));

  for (const n of data.nodes) {
    if (n.shape === "noteGroup") continue;
    const id = ourId(n.id);
    const line = lineOf(source, n.id.split("----")[0]);
    if (n.isGroup) {
      const kinds = classesOf(n).filter((c) => c in ZONE_KINDS);
      const title = plain(n.label);
      diagram.zones.set(id, {
        id,
        ...(kinds.length ? { kind: kinds[kinds.length - 1] as ZoneKind } : {}),
        ...(title && title !== n.id ? { title } : {}),
        ...(zoneOf(n) ? { parent: zoneOf(n) } : {}),
        line,
      });
      continue;
    }
    const node: DiagramNode = { id, addBadges: [], removeBadges: [], line };
    if (n.shape === "stateStart") node.kind = "start";
    else if (n.shape === "stateEnd") node.kind = "end";
    else if (n.shape === "note") node.kind = "note";
    else if (n.shape === "choice") node.kind = "diamond";
    else node.kind = "box";
    if (node.kind !== "start" && node.kind !== "end") {
      const [title, ...rest] = plain(n.label).split("\n");
      node.title = title || n.id;
      if (rest.length) node.detail = rest.join(" · ");
      applyClasses(node, classesOf(n), warnings, n.id);
    }
    const zone = zoneOf(n);
    if (zone) node.zone = zone;
    diagram.nodes.set(id, merge(extras, node));
  }

  for (const e of data.edges) {
    const from = ourId(e.start);
    const to = ourId(e.end);
    const toNote = byId.get(e.end)?.shape === "note";
    diagram.edges.push({
      from,
      to,
      arrow: toNote ? "--" : "->",
      ...(plain(e.label) ? { label: plain(e.label) } : {}),
      ...(toNote ? { stroke: "dotted" as const } : {}),
      line: lineOf(source, e.start),
    });
  }
  // Decorations for ids the diagram never mentions are refused, so a typo in
  // a %% line is not silently ignored.
  for (const id of extras.nodes.keys()) {
    if (!diagram.nodes.has(id)) diagram.nodes.set(id, extras.nodes.get(id) as DiagramNode);
  }
  return { diagram, warnings };
}

interface MindmapNode {
  nodeId: string;
  descr: string;
  type: number;
  icon?: string;
  class?: string;
  children: MindmapNode[];
}

/** Colours for a mindmap's first branches, in turn. */
const BRANCH_COLORS = ["blue", "green", "orange", "violet", "red", "teal", "pink", "cyan", "lime", "grape", "indigo", "yellow"];

export function mindmapToDiagram(source: string, db: unknown): { diagram: Diagram; warnings: string[] } {
  const extras = commentExtras(source);
  const root = (db as { getMindmap(): MindmapNode | null }).getMindmap();
  const warnings: string[] = [];
  const diagram: Diagram = { ...extras, nodes: new Map(), zones: new Map(), edges: [...extras.edges] };
  diagram.direction ??= "right";
  diagram.arrows ??= "curved";
  if (!root) return { diagram, warnings };
  const taken = new Set<string>();

  const visit = (n: MindmapNode, parent: string | null, color: string | undefined, depth: number) => {
    const text = plain(n.descr);
    const id = idFrom(n.nodeId && n.nodeId !== n.descr ? n.nodeId : text, taken);
    const [title, ...rest] = text.split("\n");
    const node: DiagramNode = {
      id,
      kind: depth === 0 ? "ellipse" : "box",
      title: title || id,
      ...(rest.length ? { detail: rest.join(" · ") } : {}),
      addBadges: [],
      removeBadges: [],
      ...(color ? { color } : {}),
      line: lineOf(source, (n.descr.split("\n")[0] ?? "").trim()),
    };
    if (n.icon) {
      // Mindmaps name Font Awesome and Material icons ("fa fa-book"); the
      // last word is often a lucide name too.
      const name = n.icon.trim().split(/\s+/).pop()?.replace(/^(fa|mdi|material)-/, "").toLowerCase() ?? "";
      if (ICON_NAMES.includes(name)) node.icon = name;
      else warnings.push(`icon "${n.icon}" on "${title}" is not one of ours and was left off`);
    }
    if (n.class) applyClasses(node, n.class.split(/\s+/).filter(Boolean), warnings, `"${title}"`);
    diagram.nodes.set(id, merge(extras, node));
    if (parent) diagram.edges.push({ from: parent, to: id, arrow: "--", line: node.line });
    n.children.forEach((child, i) => visit(child, id, depth === 0 ? BRANCH_COLORS[i % BRANCH_COLORS.length] : color, depth + 1));
  };
  visit(root, null, undefined, 0);
  return { diagram, warnings };
}

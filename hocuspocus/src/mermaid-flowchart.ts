// Mermaid flowcharts, in and out.
//
// In: a flowchart becomes the same diagram the diagram language produces
// (diagram-language.ts), so it is laid out, built from the shared catalogue's
// cards, routed and merged by exactly the same code (diagram-apply.ts).
// Standard Mermaid carries everything our shapes need:
//
//   dc01["DC01<br/>10.0.5.10"]:::domain-controller    a card: kind as a class,
//                                                      title, then detail
//   class dc01 compromised,red                         badges and colour
//   kms@{ icon: "key-round" }                          an icon
//   subgraph internal["Internal"] ... end              a zone
//   class internal dmz                                 its kind
//   a e1@-- "pass-the-hash" --> b                      a connection ...
//   class e1 lateral,curved                            ... its style and route
//   -.->  ==>  --o  --x  <-->  ---                      dotted, thick, heads
//
// Mermaid comments carry what Mermaid has no syntax for, in the diagram
// language: `%% remove ws142`, `%% style clean`, `%% arrows curved`, or a
// whole node line, `%% dc01 -compromised`.
//
// Out: a drawing read back as Mermaid in that same form, so an agent can
// change a line and send the text back.

import {
  BADGE_KEYS,
  CARD_KINDS,
  COLOR_NAMES,
  EDGE_STYLES,
  ICON_NAMES,
  ZONE_KINDS,
  iconsFor,
  type EdgeStyle,
  type ZoneKind,
} from "./drawing-catalogue.js";
import {
  DiagramError,
  GENERIC_KINDS,
  HEADS,
  ROUTES,
  defaultHeads,
  parseDiagram,
  type ArrowKind,
  type Diagram,
  type DiagramEdge,
  type DiagramNode,
  type DiagramProblem,
  type Route,
  type Stroke,
} from "./diagram-language.js";
import { parseMermaidText } from "./mermaid-parse.js";
import type { Scene, SceneNode, SceneZone } from "./diagram-read.js";

interface Vertex {
  id: string;
  text: string;
  classes: string[];
  type?: string;
  icon?: string;
}
interface Edge {
  id?: string;
  start: string;
  end: string;
  text: string;
  type: string;
  stroke?: string;
  classes?: string[];
}
interface SubGraph {
  id: string;
  title: string;
  nodes: string[];
  classes?: string[];
}
interface FlowDb {
  getVertices(): Map<string, Vertex>;
  getEdges(): Edge[];
  getSubGraphs(): SubGraph[];
  getDirection(): string;
}

const KINDS: ReadonlySet<string> = new Set([...CARD_KINDS, ...GENERIC_KINDS]);

/** Mermaid's shapes, for a node that names no kind. */
const SHAPE_KINDS: Record<string, string> = {
  cylinder: "database",
  circle: "ellipse",
  doublecircle: "ellipse",
  ellipse: "ellipse",
  stadium: "box",
  round: "box",
  diamond: "diamond",
  rhombus: "diamond",
  hexagon: "box",
  square: "box",
};

/** A Mermaid label as plain text: line breaks kept, tags and Markdown
 * emphasis dropped, entities decoded. */
function plain(text: string): string {
  return text
    .replace(/<br\s*\/?>/gi, "\n")
    .replace(/<[^>]+>/g, "")
    .replace(/#quot;/g, '"')
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/^`|`$/g, "")
    .replace(/\*\*(.+?)\*\*/g, "$1")
    .replace(/\*(.+?)\*/g, "$1")
    .trim();
}

/** The line of the text that first mentions an id, for error messages. */
function lineOf(source: string, id: string): number {
  const re = new RegExp(`(^|[^A-Za-z0-9_])${id.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}([^A-Za-z0-9_]|$)`);
  const index = source.split(/\r?\n/).findIndex((line) => re.test(line));
  return index + 1;
}

/** Whether the text gives a node a label or shape of its own, rather than
 * only naming it in a connection. A connection must not retitle a node. */
function declares(source: string, id: string): boolean {
  const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(^|[^A-Za-z0-9_])${escaped}\\s*(\\[|\\(|\\{|>|@\\{)`).test(source);
}

/** Mermaid's error, as problems with lines. Its message names the line. */
function mermaidProblems(err: unknown): DiagramProblem[] {
  const message = err instanceof Error ? err.message : String(err);
  const line = Number(/line (\d+)/i.exec(message)?.[1] ?? 0);
  return [{ line, message: `Mermaid could not parse this: ${message.split("\n").slice(0, 4).join(" ")}` }];
}

/**
 * A flowchart as a diagram. Throws a DiagramError naming every problem.
 * `aliases` maps ids a read gave nodes that have none of their own (see
 * writeMermaid) back to their element ids.
 */
export async function flowchartToDiagram(
  source: string,
  aliases: ReadonlyMap<string, string> = new Map(),
): Promise<{ diagram: Diagram; warnings: string[] }> {
  let db: FlowDb;
  try {
    db = (await parseMermaidText(source)).db as FlowDb;
  } catch (err) {
    throw new DiagramError(mermaidProblems(err));
  }
  const problems: DiagramProblem[] = [];
  const warnings: string[] = [];
  const real = (id: string) => aliases.get(id) ?? id;

  // What Mermaid has no syntax for comes in comments, in the diagram language.
  const extras = source
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("%%") && !line.startsWith("%%{"))
    .map((line) => line.replace(/^%%\s?/, ""))
    .join("\n");
  let diagram: Diagram;
  try {
    diagram = parseDiagram(extras);
  } catch (err) {
    if (err instanceof DiagramError) {
      throw new DiagramError(err.problems.map((p) => ({ ...p, message: `in a %% comment: ${p.message}` })));
    }
    throw err;
  }
  const direction = db.getDirection();
  diagram.direction ??= direction === "TB" || direction === "TD" || direction === "BT" ? "down" : "right";

  // Zones, and which zone each node is innermost in.
  const subgraphs = db.getSubGraphs();
  const subgraphIds = new Set(subgraphs.map((g) => g.id));
  const parentOf = new Map<string, string>();
  for (const g of subgraphs) {
    for (const member of g.nodes) {
      // Innermost wins: a subgraph listed later inside an earlier one is
      // nested, and Mermaid lists nested members in both.
      const current = parentOf.get(member);
      const currentSize = current ? (subgraphs.find((s) => s.id === current)?.nodes.length ?? Infinity) : Infinity;
      if (g.nodes.length <= currentSize) parentOf.set(member, g.id);
    }
  }
  for (const g of subgraphs) {
    const kinds = (g.classes ?? []).filter((c) => c in ZONE_KINDS);
    for (const c of g.classes ?? []) if (!(c in ZONE_KINDS)) warnings.push(`class "${c}" on subgraph ${g.id} means nothing here; zone kinds: ${Object.keys(ZONE_KINDS).join(", ")}`);
    const title = plain(g.title ?? "");
    diagram.zones.set(real(g.id), {
      id: real(g.id),
      ...(kinds.length ? { kind: kinds[kinds.length - 1] as ZoneKind } : {}),
      ...(title && title !== g.id ? { title } : {}),
      ...(parentOf.has(g.id) ? { parent: real(parentOf.get(g.id) as string) } : {}),
      line: lineOf(source, g.id),
    });
  }

  // Nodes.
  for (const v of db.getVertices().values()) {
    if (subgraphIds.has(v.id)) continue;
    const id = real(v.id);
    const line = lineOf(source, v.id);
    const classes = v.classes.flatMap((c) => c.split(",")).map((c) => c.trim()).filter(Boolean);
    const node: DiagramNode = diagram.nodes.get(id) ?? { id, addBadges: [], removeBadges: [], line };
    for (const c of classes) {
      const k = c.toLowerCase();
      if (KINDS.has(k)) node.kind = k;
      else if (BADGE_KEYS.includes(k)) node.addBadges.push(k);
      else if (COLOR_NAMES.includes(k)) node.color = k;
      else warnings.push(`class "${c}" on ${v.id} means nothing here`);
    }
    if (!node.kind && v.type && SHAPE_KINDS[v.type]) node.kind = SHAPE_KINDS[v.type];
    if (v.icon) {
      const name = v.icon.replace(/^[a-z0-9-]+:/i, "").toLowerCase();
      if (ICON_NAMES.includes(name)) node.icon = name;
      else {
        const fits = iconsFor(name);
        problems.push({ line, message: `unknown icon "${v.icon}"${fits.length ? `. Icons that fit: ${fits.join(", ")}` : ""}` });
      }
    }
    if (declares(source, v.id)) {
      const [title, ...rest] = plain(v.text).split("\n");
      node.title = title || v.id;
      if (rest.length > 0) node.detail = rest.join(" · ");
    }
    if (parentOf.has(v.id)) node.zone = real(parentOf.get(v.id) as string);
    diagram.nodes.set(id, node);
  }

  // Connections.
  for (const e of db.getEdges()) {
    if (e.stroke === "invisible") continue;
    const line = lineOf(source, e.start);
    const kind: Record<string, { arrow: ArrowKind; head?: string; tail?: string }> = {
      arrow_point: { arrow: "->" },
      arrow_open: { arrow: "--" },
      double_arrow_point: { arrow: "<->" },
      arrow_circle: { arrow: "->", head: "circle" },
      arrow_cross: { arrow: "->", head: "bar" },
      double_arrow_circle: { arrow: "<->", head: "circle", tail: "circle" },
      double_arrow_cross: { arrow: "<->", head: "bar", tail: "bar" },
    };
    const shape = kind[e.type] ?? { arrow: "->" as ArrowKind };
    const edge: DiagramEdge = {
      from: real(e.start),
      to: real(e.end),
      arrow: shape.arrow,
      ...(shape.head ? { head: shape.head } : {}),
      ...(shape.tail ? { tail: shape.tail } : {}),
      ...(e.text ? { label: plain(e.text) } : {}),
      ...(e.stroke === "dotted" || e.stroke === "thick" ? { stroke: e.stroke as Stroke } : {}),
      line,
    };
    for (const c of (e.classes ?? []).flatMap((x) => x.split(",")).map((x) => x.trim()).filter(Boolean)) {
      const k = c.toLowerCase();
      if (k in EDGE_STYLES) edge.style = k as EdgeStyle;
      else if ((ROUTES as readonly string[]).includes(k)) edge.route = k as Route;
      else if (/^(head|tail)-/.test(k) && (HEADS as readonly string[]).includes(k.slice(5))) edge[k.slice(0, 4) as "head" | "tail"] = k.slice(5);
      else warnings.push(`class "${c}" on connection ${e.id ?? `${e.start} -> ${e.end}`} means nothing here`);
    }
    diagram.edges.push(edge);
  }

  if (problems.length > 0) throw new DiagramError(problems);
  return { diagram, warnings };
}

// ---------------------------------------------------------------------------
// Out

const MERMAID_WORDS = new Set(["end", "graph", "flowchart", "subgraph", "class", "classdef", "style", "linkstyle", "click", "direction", "default"]);

/** An id Mermaid can take as it is: letters, digits and underscores, and not
 * one of its own words. Element ids can hold "-", which Mermaid reads as part
 * of an arrow; those are written as n_<id with _ for ->, and mapped back on
 * the next write. */
export function mermaidId(id: string): string {
  if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(id) && !MERMAID_WORDS.has(id.toLowerCase())) return id;
  return `n_${id.replace(/[^A-Za-z0-9_]/g, "_")}`;
}

/** The aliases a read gave, so a write can find the nodes again. */
export function aliasesOf(scene: Scene): Map<string, string> {
  const out = new Map<string, string>();
  for (const id of [...scene.nodes.keys(), ...scene.zones.keys()]) {
    const alias = mermaidId(id);
    if (alias !== id) out.set(alias, id);
  }
  return out;
}

function label(text: string): string {
  return `"${text.replace(/"/g, "#quot;").replace(/\n/g, "<br/>")}"`;
}

/** Mermaid's links, by line: normal, dotted (-.-) and thick (===). */
const LINKS: Record<"normal" | Stroke, { open: string; arrow: string; both: string; circle: string; circles: string; cross: string; crosses: string }> = {
  normal: { open: "---", arrow: "-->", both: "<-->", circle: "--o", circles: "o--o", cross: "--x", crosses: "x--x" },
  dotted: { open: "-.-", arrow: "-.->", both: "<-.->", circle: "-.-o", circles: "o-.-o", cross: "-.-x", crosses: "x-.-x" },
  thick: { open: "===", arrow: "==>", both: "<==>", circle: "==o", circles: "o==o", cross: "==x", crosses: "x==x" },
};

/** A drawing's nodes, zones and connections, as a Mermaid flowchart. */
export function writeMermaid(scene: Scene, direction: "right" | "down" = "right"): string {
  const lines: string[] = [`flowchart ${direction === "down" ? "TB" : "LR"}`];
  const nodes = [...scene.nodes.values()];
  if (nodes.length > 0 && nodes.every((n) => n.style === "clean")) lines.push("  %% style clean");
  const routes = new Map<Route, number>();
  for (const e of scene.edges) routes.set(e.route, (routes.get(e.route) ?? 0) + 1);
  const usual: Route = [...routes.entries()].sort((a, b) => b[1] - a[1] || (a[0] === "elbow" ? -1 : 1))[0]?.[0] ?? "elbow";
  if (usual !== "elbow") lines.push(`  %% arrows ${usual}`);

  const classLines: string[] = [];
  const nodeLine = (n: SceneNode, pad: string) => {
    const id = mermaidId(n.id);
    const text = n.detail ? `${n.title}\n${n.detail}` : n.title;
    const open = n.kind === "ellipse" ? "([" : n.kind === "diamond" ? "{" : "[";
    const close = n.kind === "ellipse" ? "])" : n.kind === "diamond" ? "}" : "]";
    // Plain boxes, ellipses and diamonds are said by their shape; anything
    // else names its kind.
    const kindClass = n.kind === "ellipse" || n.kind === "diamond" || n.kind === "box" ? "" : `:::${n.kind}`;
    lines.push(`${pad}${id}${open}${label(text)}${close}${kindClass}`);
    if (n.icon) lines.push(`${pad}${id}@{ icon: "${n.icon}" }`);
    const extra = [...n.badges, ...(n.color ? [n.color] : [])];
    if (extra.length > 0) classLines.push(`  class ${id} ${extra.join(",")}`);
  };
  const zone = (z: SceneZone, depth: number) => {
    const pad = "  ".repeat(depth);
    lines.push(`${pad}subgraph ${mermaidId(z.id)}[${label(z.title)}]`);
    for (const inner of [...scene.zones.values()].filter((c) => c.parent === z.id)) zone(inner, depth + 1);
    for (const n of nodes.filter((n) => n.zone === z.id)) nodeLine(n, `${pad}  `);
    lines.push(`${pad}end`);
    classLines.push(`  class ${mermaidId(z.id)} ${z.kind}`);
  };
  for (const z of [...scene.zones.values()].filter((z) => !z.parent)) zone(z, 1);
  for (const n of nodes.filter((n) => !n.zone)) nodeLine(n, "  ");

  scene.edges.forEach((e, i) => {
    const implied = defaultHeads(e.arrow);
    const forms = LINKS[e.stroke ?? "normal"];
    let link: string;
    let head = e.head;
    let tail = e.tail;
    // Heads Mermaid draws itself: circle and cross ends, and both ways.
    const circle = head === "circle" && (e.arrow !== "<->" || tail === "circle");
    const cross = head === "bar" && (e.arrow !== "<->" || tail === "bar");
    const both = e.arrow === "<->";
    if (e.arrow === "--" && head === "none" && tail === "none") link = forms.open;
    else if (circle) link = both ? forms.circles : forms.circle;
    else if (cross) link = both ? forms.crosses : forms.cross;
    else link = both ? forms.both : forms.arrow;
    if (circle || cross) {
      head = implied.head;
      tail = implied.tail;
    } else if (e.arrow === "--") {
      head = head === "none" ? implied.head : head;
    }
    const classes = [
      ...(e.style !== "plain" ? [e.style] : []),
      ...(e.route !== usual ? [e.route] : []),
      ...(head !== implied.head && head !== "arrow" ? [`head-${head}`] : []),
      ...(tail !== implied.tail ? [`tail-${tail}`] : []),
    ];
    const edgeId = classes.length > 0 ? `e${i + 1}@` : "";
    const text = e.label ? `|${label(e.label)}|` : "";
    lines.push(`  ${mermaidId(e.from)} ${edgeId}${link}${text} ${mermaidId(e.to)}`);
    if (classes.length > 0) classLines.push(`  class e${i + 1} ${classes.join(",")}`);
  });
  return [...lines, ...classLines].join("\n");
}

export type { ZoneKind };

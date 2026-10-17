// The diagram language: how an agent reads and writes a drawing as text.
//
// Writing Excalidraw JSON asks a model to place every shape by coordinate,
// invent ids, wire label and arrow bindings in both directions and keep paint
// order straight. Frontier models manage; smaller ones misplace, overlap and
// mis-wire, and reading a drawing back as a few hundred lines of geometry is
// worse. This language asks for what the diagram *is* and nothing else:
//
//   direction right
//   zone internal "Internal — 10.0.0.0/24" network {
//     dc01: domain-controller "DC01" "10.0.5.10"
//     ws142: workstation "WS-0142" "Windows 11"
//   }
//   attacker: attacker "Operator"
//   attacker -> ws142 "phishing" [exploit]
//   ws142 -> dc01 "pass-the-hash" [lateral]
//   dc01 +compromised
//
// The server places the shapes, builds the cards from the shared catalogue
// (drawing-catalogue.ts), and binds the arrows (diagram-apply.ts). A read
// produces the same text (diagram-read.ts), so an agent can read a drawing,
// change a line and send it back.
//
// One statement per line:
//
//   ID: KIND "title" "detail" +badge     a node; title and detail optional
//   ... color=red icon=key-round         its colour and icon, on any node line
//   ID: "title"                          retitle a node that exists
//   ID +badge -badge                     add or remove badges
//   A -> B "label" [style]               a connection; also <-> and --
//   A -> B [lateral curved head=triangle] route and arrowheads, in the brackets
//   zone ID "title" KIND { ... }         a region holding the nodes inside it
//   remove ID | remove A -> B            erase a node, zone or connection
//   direction right|down                 layout direction for new shapes
//   style sketch|clean                   the look of new shapes
//   arrows elbow|curved|straight         how new connections are drawn
//   # comment
//
// Parsing is all-or-nothing: a text with problems changes nothing, and every
// problem comes back with its line number, so one round trip fixes them all.

import {
  BADGE_KEYS,
  CARD_KINDS,
  COLOR_NAMES,
  EDGE_STYLES,
  ICON_NAMES,
  ZONE_KINDS,
  iconsFor,
  isColor,
  type EdgeStyle,
  type ShapeStyle,
  type ZoneKind,
} from "./drawing-catalogue.js";

/** Kinds that are not cards: a plain labelled shape, and a sticky note. */
export const GENERIC_KINDS = ["box", "ellipse", "diamond", "note"] as const;
export type GenericKind = (typeof GENERIC_KINDS)[number];

export type ArrowKind = "->" | "<->" | "--";

/** How a connection is drawn. Elbow runs horizontally and vertically around
 * other shapes; it is the default. */
export const ROUTES = ["elbow", "curved", "straight"] as const;
export type Route = (typeof ROUTES)[number];

/** Arrowheads, as the language names them; Excalidraw's own names use _ for
 * the - here. */
export const HEADS = [
  "arrow",
  "triangle",
  "triangle-outline",
  "circle",
  "circle-outline",
  "diamond",
  "diamond-outline",
  "bar",
  "crowfoot-one",
  "crowfoot-many",
  "crowfoot-one-or-many",
  "none",
] as const;

export interface DiagramNode {
  id: string;
  kind?: string;
  title?: string;
  detail?: string;
  addBadges: string[];
  removeBadges: string[];
  /** A palette name or #rrggbb; null puts the kind's own colour back. */
  color?: string | null;
  /** A lucide icon name; null takes a chosen icon off. */
  icon?: string | null;
  /** The zone this node was declared inside, if any. */
  zone?: string;
  line: number;
}

export interface DiagramZone {
  id: string;
  kind?: ZoneKind;
  title?: string;
  parent?: string;
  line: number;
}

export interface DiagramEdge {
  from: string;
  to: string;
  arrow: ArrowKind;
  label?: string;
  style?: EdgeStyle;
  route?: Route;
  /** The arrowhead at the end; "none" for none. */
  head?: string;
  /** The arrowhead at the start. */
  tail?: string;
  line: number;
}

export type DiagramRemoval =
  | { kind: "item"; id: string; line: number }
  | { kind: "edge"; from: string; to: string; line: number };

export interface Diagram {
  direction?: "right" | "down";
  style?: ShapeStyle;
  /** How connections this text adds are drawn, unless one says otherwise. */
  arrows?: Route;
  nodes: Map<string, DiagramNode>;
  zones: Map<string, DiagramZone>;
  edges: DiagramEdge[];
  removals: DiagramRemoval[];
}

export interface DiagramProblem {
  line: number;
  message: string;
}

export class DiagramError extends Error {
  constructor(readonly problems: DiagramProblem[]) {
    super(problems.map((p) => `line ${p.line}: ${p.message}`).join("\n"));
  }
}

// ---------------------------------------------------------------------------
// Tokens

type Token =
  | { t: "id"; v: string }
  | { t: "str"; v: string }
  | { t: "colon" }
  | { t: "open" }
  | { t: "close" }
  | { t: "arrow"; v: ArrowKind }
  | { t: "style"; v: string }
  | { t: "badge"; v: string; add: boolean }
  | { t: "attr"; k: string; v: string };

const ID_RE = /^[A-Za-z0-9_][A-Za-z0-9_.\-]*/;

function tokenize(line: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  while (i < line.length) {
    const ch = line[i];
    const rest = line.slice(i);
    if (/\s/.test(ch)) {
      i++;
    } else if (ch === "#") {
      break;
    } else if (ch === '"') {
      let value = "";
      i++;
      let closed = false;
      while (i < line.length) {
        if (line[i] === "\\" && i + 1 < line.length) {
          const next = line[i + 1];
          value += next === "n" ? "\n" : next;
          i += 2;
        } else if (line[i] === '"') {
          closed = true;
          i++;
          break;
        } else {
          value += line[i++];
        }
      }
      if (!closed) throw new Error("a quoted text is not closed with \"");
      out.push({ t: "str", v: value });
    } else if (rest.startsWith("<->")) {
      out.push({ t: "arrow", v: "<->" });
      i += 3;
    } else if (rest.startsWith("-->")) {
      // Mermaid's arrow, which models write out of habit.
      out.push({ t: "arrow", v: "->" });
      i += 3;
    } else if (rest.startsWith("->")) {
      out.push({ t: "arrow", v: "->" });
      i += 2;
    } else if (rest.startsWith("--")) {
      out.push({ t: "arrow", v: "--" });
      i += 2;
    } else if (ch === ":") {
      out.push({ t: "colon" });
      i++;
    } else if (ch === "{") {
      out.push({ t: "open" });
      i++;
    } else if (ch === "}") {
      out.push({ t: "close" });
      i++;
    } else if (ch === "[") {
      const end = line.indexOf("]", i);
      if (end < 0) throw new Error("a [style] is not closed with ]");
      out.push({ t: "style", v: line.slice(i + 1, end).trim() });
      i = end + 1;
    } else if ((ch === "+" || ch === "-") && ID_RE.test(line.slice(i + 1))) {
      const m = line.slice(i + 1).match(ID_RE) as RegExpMatchArray;
      out.push({ t: "badge", v: m[0], add: ch === "+" });
      i += 1 + m[0].length;
    } else {
      const m = rest.match(ID_RE);
      if (!m) throw new Error(`unexpected "${ch}"`);
      i += m[0].length;
      // key=value: an attribute. The value is a word, a #colour or a quoted
      // text.
      if (line[i] === "=") {
        i++;
        const quoted = line[i] === '"';
        const value = quoted ? line.slice(i + 1).match(/^[^"]*/)?.[0] ?? "" : line.slice(i).match(/^[#A-Za-z0-9_.\-]*/)?.[0] ?? "";
        if (value === "") throw new Error(`${m[0]}= needs a value`);
        i += value.length + (quoted ? 2 : 0);
        out.push({ t: "attr", k: m[0].toLowerCase(), v: value });
      } else {
        out.push({ t: "id", v: m[0] });
      }
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Parsing

const DIRECTIONS: Record<string, "right" | "down"> = {
  right: "right",
  lr: "right",
  down: "down",
  td: "down",
  tb: "down",
};

const KNOWN_KINDS: ReadonlySet<string> = new Set([...CARD_KINDS, ...GENERIC_KINDS]);

/** The closest few names to a misspelt one, for the error message. */
function suggest(word: string, names: readonly string[]): string {
  const w = word.toLowerCase();
  const scored = names
    .map((name) => ({ name, score: distance(w, name) - (name.includes(w) || w.includes(name) ? 3 : 0) }))
    .sort((a, b) => a.score - b.score)
    .slice(0, 3)
    .filter((s) => s.score <= Math.max(3, w.length / 2));
  return scored.length > 0 ? ` Did you mean ${scored.map((s) => s.name).join(", ")}?` : "";
}

function distance(a: string, b: string): number {
  const d = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array(b.length).fill(0)]);
  for (let j = 1; j <= b.length; j++) d[0][j] = j;
  for (let i = 1; i <= a.length; i++) {
    for (let j = 1; j <= b.length; j++) {
      d[i][j] = Math.min(d[i - 1][j] + 1, d[i][j - 1] + 1, d[i - 1][j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1));
    }
  }
  return d[a.length][b.length];
}

/** Parse a diagram. Throws a DiagramError listing every problem. */
export function parseDiagram(source: string): Diagram {
  const diagram: Diagram = { nodes: new Map(), zones: new Map(), edges: [], removals: [] };
  const problems: DiagramProblem[] = [];
  const zoneStack: string[] = [];

  source.split(/\r?\n/).forEach((raw, index) => {
    const line = index + 1;
    const fail = (message: string) => problems.push({ line, message });
    let tokens: Token[];
    try {
      tokens = tokenize(raw);
    } catch (err) {
      fail((err as Error).message);
      return;
    }
    if (tokens.length === 0) return;

    const [first, second] = tokens;
    const word = first.t === "id" ? first.v.toLowerCase() : "";

    // }  closes a zone
    if (first.t === "close") {
      if (tokens.length > 1) return fail("nothing may follow } on its line");
      if (zoneStack.length === 0) return fail("} without a zone to close");
      zoneStack.pop();
      return;
    }

    if (first.t !== "id") return fail("a line starts with an id, or with zone, remove, direction or style");

    // direction right|down
    if (word === "direction" && second?.t === "id" && tokens.length === 2) {
      const dir = DIRECTIONS[second.v.toLowerCase()];
      if (!dir) return fail(`direction is right or down, not "${second.v}"`);
      diagram.direction = dir;
      return;
    }

    // style sketch|clean
    if (word === "style" && second?.t === "id" && tokens.length === 2) {
      const style = second.v.toLowerCase();
      if (style !== "sketch" && style !== "clean") return fail(`style is sketch or clean, not "${second.v}"`);
      diagram.style = style;
      return;
    }

    // arrows elbow|curved|straight
    if (word === "arrows" && second?.t === "id" && tokens.length === 2) {
      const route = second.v.toLowerCase();
      if (!(ROUTES as readonly string[]).includes(route)) return fail(`arrows is ${ROUTES.join(", ")}, not "${second.v}"`);
      diagram.arrows = route as Route;
      return;
    }

    // remove ID | remove A -> B
    if (word === "remove" && second?.t === "id") {
      if (tokens.length === 2) {
        diagram.removals.push({ kind: "item", id: second.v, line });
        return;
      }
      const [, a, arrow, b] = tokens;
      if (tokens.length === 4 && a.t === "id" && arrow.t === "arrow" && b.t === "id") {
        diagram.removals.push({ kind: "edge", from: a.v, to: b.v, line });
        return;
      }
      return fail("remove takes one id, or a connection: remove a -> b");
    }

    // zone ID "title" KIND {
    if (word === "zone" && second?.t === "id") {
      const id = second.v;
      const zone: DiagramZone = { id, line, ...(zoneStack.length ? { parent: zoneStack[zoneStack.length - 1] } : {}) };
      let opens = false;
      for (const tok of tokens.slice(2)) {
        if (tok.t === "str" && zone.title === undefined) zone.title = tok.v;
        else if (tok.t === "id") {
          const kind = tok.v.toLowerCase();
          if (!(kind in ZONE_KINDS)) {
            return fail(`unknown zone kind "${tok.v}". Zone kinds: ${Object.keys(ZONE_KINDS).join(", ")}.`);
          }
          zone.kind = kind as ZoneKind;
        } else if (tok.t === "open") opens = true;
        else return fail('a zone is: zone id "title" kind {');
      }
      if (diagram.zones.has(id) || diagram.nodes.has(id)) return fail(`"${id}" is declared twice`);
      diagram.zones.set(id, zone);
      if (opens) zoneStack.push(id);
      return;
    }

    // A -> B "label" [style]
    if (second?.t === "arrow") {
      const target = tokens[2];
      if (target?.t !== "id") return fail(`a connection is: ${first.v} ${second.v} other_id "label" [style]`);
      const edge: DiagramEdge = { from: first.v, to: target.v, arrow: second.v, line };
      // Words that describe a connection, inside [ ] or bare: its style, its
      // route, and head= / tail= for its arrowheads.
      const describe = (word: string): string | null => {
        const w = word.toLowerCase();
        const eq = w.indexOf("=");
        if (eq > 0) {
          const k = w.slice(0, eq);
          const v = w.slice(eq + 1);
          if (k !== "head" && k !== "tail") return `unknown setting "${k}=". Connections take head= and tail=.`;
          if (!(HEADS as readonly string[]).includes(v)) return `unknown arrowhead "${v}". Arrowheads: ${HEADS.join(", ")}.`;
          edge[k] = v;
          return null;
        }
        if (w in EDGE_STYLES) edge.style = w as EdgeStyle;
        else if ((ROUTES as readonly string[]).includes(w)) edge.route = w as Route;
        else return `unknown word "${word}" for a connection. Styles: ${Object.keys(EDGE_STYLES).join(", ")}. Routes: ${ROUTES.join(", ")}.`;
        return null;
      };
      for (const tok of tokens.slice(3)) {
        if (tok.t === "str" && edge.label === undefined) edge.label = tok.v;
        else if (tok.t === "style" || tok.t === "id") {
          for (const word of tok.v.split(/[\s,]+/).filter(Boolean)) {
            const problem = describe(word);
            if (problem) return fail(problem);
          }
        } else if (tok.t === "attr") {
          const problem = describe(`${tok.k}=${tok.v}`);
          if (problem) return fail(problem);
        } else if (tok.t === "arrow") {
          return fail("one connection per line: write a -> b and b -> c on separate lines");
        } else return fail(`a connection is: ${first.v} ${second.v} ${target.v} "label" [style]`);
      }
      if (edge.from === edge.to) return fail("a connection needs two different ends");
      diagram.edges.push(edge);
      return;
    }

    // ID +badge -badge color=red     (no colon)
    if (second?.t === "badge" || second?.t === "attr") {
      const node = nodeFor(first.v);
      if (!node) return;
      for (const tok of tokens.slice(1)) {
        if (tok.t === "badge") {
          if (!addBadge(node, tok)) return;
        } else if (tok.t === "attr") {
          if (!setAttr(node, tok)) return;
        } else {
          return fail("after an id without a colon, only +badge, -badge, color= and icon= may follow");
        }
      }
      return;
    }

    // ID: KIND "title" "detail" +badge
    if (second?.t === "colon") {
      const node = nodeFor(first.v);
      if (!node) return;
      let strings = 0;
      for (const tok of tokens.slice(2)) {
        if (tok.t === "id") {
          if (strings > 0 || node.kind !== undefined && node.line === line) {
            return fail(`the kind comes before the title: ${first.v}: server "title"`);
          }
          const kind = tok.v.toLowerCase();
          if (!KNOWN_KINDS.has(kind)) {
            return fail(`unknown kind "${tok.v}".${suggest(kind, [...KNOWN_KINDS])} Kinds: ${[...KNOWN_KINDS].join(", ")}.`);
          }
          node.kind = kind;
        } else if (tok.t === "str") {
          if (strings === 0) node.title = tok.v;
          else if (strings === 1) node.detail = tok.v;
          else return fail('a node takes at most two texts: "title" "detail"');
          strings++;
        } else if (tok.t === "badge") {
          if (!addBadge(node, tok)) return;
        } else if (tok.t === "attr") {
          if (!setAttr(node, tok)) return;
        } else {
          return fail(`a node is: ${first.v}: kind "title" "detail"`);
        }
      }
      if (zoneStack.length > 0) node.zone = zoneStack[zoneStack.length - 1];
      return;
    }

    return fail(
      `"${raw.trim()}" is not a statement. A node is id: kind "title", a connection is a -> b, a zone is zone id "title" { … }`,
    );

    function nodeFor(id: string): DiagramNode | null {
      if (diagram.zones.has(id)) {
        fail(`"${id}" is a zone, not a node`);
        return null;
      }
      let node = diagram.nodes.get(id);
      if (!node) {
        node = { id, addBadges: [], removeBadges: [], line };
        diagram.nodes.set(id, node);
      }
      return node;
    }

    function setAttr(node: DiagramNode, tok: Extract<Token, { t: "attr" }>): boolean {
      const value = tok.v;
      if (tok.k === "color" || tok.k === "colour") {
        if (value === "default" || value === "none") node.color = null;
        else if (isColor(value)) node.color = value.toLowerCase();
        else {
          fail(`unknown colour "${value}". Colours: ${COLOR_NAMES.join(", ")}, or #rrggbb.`);
          return false;
        }
        return true;
      }
      if (tok.k === "icon") {
        if (value === "none") node.icon = null;
        else if (ICON_NAMES.includes(value.toLowerCase())) node.icon = value.toLowerCase();
        else {
          const fits = iconsFor(value);
          // No spelling fallback: "vault" is not a misspelt "bolt", and an
          // icon chosen by spelling would mean the wrong thing.
          fail(
            `unknown icon "${value}". ` +
              (fits.length > 0
                ? `Icons that fit: ${fits.join(", ")}.`
                : "Name it by what it shows, e.g. lock, key-round, shield, server, cloud, user, bug, globe.") +
              " Icons are lucide's names (lucide.dev); a few hundred of them are available.",
          );
          return false;
        }
        return true;
      }
      fail(`unknown attribute "${tok.k}=". Nodes take color= and icon=.`);
      return false;
    }

    function addBadge(node: DiagramNode, tok: Extract<Token, { t: "badge" }>): boolean {
      const key = tok.v.toLowerCase();
      if (!BADGE_KEYS.includes(key)) {
        fail(`unknown badge "${tok.v}".${suggest(key, BADGE_KEYS)} Badges: ${BADGE_KEYS.join(", ")}.`);
        return false;
      }
      (tok.add ? node.addBadges : node.removeBadges).push(key);
      return true;
    }
  });

  if (zoneStack.length > 0) {
    const open = diagram.zones.get(zoneStack[zoneStack.length - 1]);
    problems.push({ line: open?.line ?? 0, message: `zone "${zoneStack[zoneStack.length - 1]}" is never closed with }` });
  }
  if (problems.length > 0) throw new DiagramError(problems.slice(0, 20));
  return diagram;
}

// ---------------------------------------------------------------------------
// Writing

export function quote(text: string): string {
  return `"${text.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n")}"`;
}

export interface SerializedNode {
  id: string;
  kind: string;
  title: string;
  detail: string;
  badges: string[];
  color?: string;
  icon?: string;
}

export interface SerializedZone {
  id: string;
  kind: ZoneKind;
  title: string;
  zones: SerializedZone[];
  nodes: SerializedNode[];
}

export interface SerializedEdge {
  from: string;
  to: string;
  arrow: ArrowKind;
  label: string;
  style: EdgeStyle;
  /** Written when it differs from the text's `arrows` line. */
  route?: Route;
  /** Written when they differ from what the arrow kind implies. */
  head?: string;
  tail?: string;
}

export function nodeLine(node: SerializedNode): string {
  const parts = [`${node.id}: ${node.kind}`];
  if (node.title || node.detail) parts.push(quote(node.title));
  if (node.detail) parts.push(quote(node.detail));
  for (const badge of node.badges) parts.push(`+${badge}`);
  if (node.color) parts.push(`color=${node.color}`);
  if (node.icon) parts.push(`icon=${node.icon}`);
  return parts.join(" ");
}

export function edgeLine(edge: SerializedEdge): string {
  const parts = [`${edge.from} ${edge.arrow} ${edge.to}`];
  if (edge.label) parts.push(quote(edge.label));
  const words = [
    ...(edge.style !== "plain" ? [edge.style] : []),
    ...(edge.route ? [edge.route] : []),
    ...(edge.head ? [`head=${edge.head}`] : []),
    ...(edge.tail ? [`tail=${edge.tail}`] : []),
  ];
  if (words.length > 0) parts.push(`[${words.join(" ")}]`);
  return parts.join(" ");
}

/** The arrowheads an arrow kind implies, before any head= or tail=. */
export function defaultHeads(arrow: ArrowKind): { head: string; tail: string } {
  return { head: arrow === "--" ? "none" : "arrow", tail: arrow === "<->" ? "arrow" : "none" };
}

/** Write a diagram out as text, zones first with their contents indented. */
export function writeDiagram(input: {
  style?: ShapeStyle;
  arrows?: Route;
  zones: SerializedZone[];
  nodes: SerializedNode[];
  edges: SerializedEdge[];
}): string {
  const lines: string[] = [];
  if (input.style === "clean") lines.push("style clean");
  if (input.arrows && input.arrows !== "elbow") lines.push(`arrows ${input.arrows}`);
  const writeZone = (zone: SerializedZone, depth: number) => {
    const pad = "  ".repeat(depth);
    lines.push(`${pad}zone ${zone.id} ${quote(zone.title)} ${zone.kind} {`);
    for (const inner of zone.zones) writeZone(inner, depth + 1);
    for (const node of zone.nodes) lines.push(`${pad}  ${nodeLine(node)}`);
    lines.push(`${pad}}`);
  };
  for (const zone of input.zones) writeZone(zone, 0);
  for (const node of input.nodes) lines.push(nodeLine(node));
  for (const edge of input.edges) lines.push(edgeLine(edge));
  return lines.join("\n");
}

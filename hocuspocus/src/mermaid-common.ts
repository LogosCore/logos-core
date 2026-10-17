// What every Mermaid diagram type shares on its way onto a drawing.

import { DiagramError, parseDiagram, type Diagram, type DiagramProblem } from "./diagram-language.js";

/** Mermaid's diagram types, by the word that opens one. The ones drawn here,
 * and the rest, which are refused by name rather than with a parse error
 * from a language they are not written in. */
export const MERMAID_TYPES: Record<string, string> = {
  flowchart: "flowchart",
  graph: "flowchart",
  sequenceDiagram: "sequence",
  stateDiagram: "state",
  "stateDiagram-v2": "state",
  mindmap: "mindmap",
  timeline: "timeline",
  gantt: "gantt",
  classDiagram: "class",
  "classDiagram-v2": "class",
  erDiagram: "er",
  journey: "journey",
  pie: "pie",
  gitGraph: "git",
  quadrantChart: "quadrant",
  requirementDiagram: "requirement",
  C4Context: "c4",
  C4Container: "c4",
  C4Component: "c4",
  C4Dynamic: "c4",
  C4Deployment: "c4",
  sankey: "sankey",
  "sankey-beta": "sankey",
  "xychart-beta": "xychart",
  xychart: "xychart",
  "block-beta": "block",
  block: "block",
  packet: "packet",
  "packet-beta": "packet",
  kanban: "kanban",
  "architecture-beta": "architecture",
  radar: "radar",
  "radar-beta": "radar",
  treemap: "treemap",
  "treemap-beta": "treemap",
};

/** The types that can be drawn. */
export const DRAWN_TYPES = ["flowchart", "sequence", "state", "mindmap", "timeline", "gantt"] as const;
export type DrawnType = (typeof DRAWN_TYPES)[number];

/** The Mermaid type a text opens with, or null when it is not Mermaid. */
export function mermaidType(text: string): string | null {
  const first = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line !== "" && !line.startsWith("%%") && !line.startsWith("#") && !line.startsWith("---"));
  const word = first?.split(/\s/)[0];
  return word && word in MERMAID_TYPES ? MERMAID_TYPES[word] : null;
}

/** A Mermaid label as plain text: line breaks kept, tags and Markdown
 * emphasis dropped, entities decoded. */
export function plain(text: unknown): string {
  return String(text ?? "")
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

/** An id made of what Mermaid lets agents write freely ("VPN portal",
 * "Foothold"), usable in the diagram language and in Mermaid both. */
export function idFrom(text: string, taken: Set<string>): string {
  const base = text.toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "").slice(0, 32) || "node";
  const start = /^[a-z_]/.test(base) ? base : `n_${base}`;
  let id = start;
  for (let i = 2; taken.has(id); i++) id = `${start}_${i}`;
  taken.add(id);
  return id;
}

/** Mermaid's error, as problems with lines. Its message names the line. */
export function mermaidProblems(err: unknown): DiagramProblem[] {
  const message = err instanceof Error ? err.message : String(err);
  const line = Number(/line (\d+)/i.exec(message)?.[1] ?? 0);
  return [{ line, message: `Mermaid could not parse this: ${message.split("\n").slice(0, 4).join(" ")}` }];
}

/** What Mermaid has no syntax for comes in %% comments, in the diagram
 * language. Returns them parsed, or throws with the comment named. */
export function commentExtras(source: string): Diagram {
  const extras = source
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("%%") && !line.startsWith("%%{"))
    .map((line) => line.replace(/^%%\s?/, ""))
    .join("\n");
  try {
    return parseDiagram(extras);
  } catch (err) {
    if (err instanceof DiagramError) {
      throw new DiagramError(err.problems.map((p) => ({ ...p, message: `in a %% comment: ${p.message}` })));
    }
    throw err;
  }
}

/** The line of the text that first mentions a word, for error messages. */
export function lineOf(source: string, word: string): number {
  const escaped = word.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp(`(^|[^A-Za-z0-9_])${escaped}([^A-Za-z0-9_]|$)`);
  return source.split(/\r?\n/).findIndex((line) => re.test(line)) + 1;
}

/** A slug for a fixed diagram's id: its title, else its type. */
export function slug(text: string, fallback: string): string {
  return text.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || fallback;
}

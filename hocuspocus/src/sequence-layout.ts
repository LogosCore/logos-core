// Mermaid sequence diagrams, drawn as Excalidraw shapes.
//
// A conversation drawn as a graph turns into a tangle: every message between
// the same two parties is another line between the same two boxes, and order
// survives only as numbers in the labels. A sequence diagram keeps the
// parties as columns and gives each message its own row, top to bottom, so
// order is position and no two labels meet.
//
// Placement is fixed rather than searched: a column per participant, a row
// per message, note, block divider. Participants are cards from the shared
// catalogue (an actor is a `user` card; a `%% spa: web-app` comment picks
// any kind, colour or icon, in the diagram language); everything else is
// plain Excalidraw: dashed lifelines, horizontal arrows, notes, activation
// bars, and frames for loop / alt / opt / par / critical / break.
//
// Every part is tagged with the sequence's id (its `title`, else
// "sequence"), and the Mermaid it was drawn from is kept on its lifelines.
// That is how a read returns the source (diagram-read.ts) and how sending
// the same id again replaces the diagram where it stands.

import type * as Y from "yjs";

import { buildCard, CARD_HEIGHT, CARD_KINDS, buildZone, type ShapeStyle, type ShapeTag } from "./drawing-catalogue.js";
import { DiagramError, parseDiagram, type DiagramNode } from "./diagram-language.js";
import { boundTextFields, measureText, normalizeElement, type NormalizedElement } from "./drawing-elements.js";
import { skeletonsToElements } from "./drawing-skeleton.js";
import { parseMermaidText } from "./mermaid-parse.js";
import { fixedTagger, placeFixed, sourceTag } from "./fixed-diagram.js";
import type { Box, Scene } from "./diagram-read.js";

interface Actor {
  name: string;
  description?: string;
  type?: string;
  box?: { name?: string };
}
interface Message {
  from?: string;
  to?: string;
  message?: string | { start?: number; step?: number; visible?: boolean };
  type: number;
  placement?: number;
}
interface SequenceDb {
  getActorKeys(): string[];
  getActors(): Map<string, Actor>;
  getMessages(): Message[];
  getBoxes(): { name?: string; actorKeys: string[] }[];
  getDiagramTitle?(): string;
  LINETYPE: Record<string, number>;
  PLACEMENT: Record<string, number>;
}

const GAP = 90;
const ROW = 58;
const BLOCK_ROW = 34;
const NOTE_WIDTH = 220;
const ACTIVATION_WIDTH = 12;
const FONT = { sketch: 5, clean: 6 } as const;

export interface SequenceResult {
  id: string;
  participants: number;
  messages: number;
  warnings: string[];
}

function slug(text: string): string {
  return text.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "sequence";
}

function plain(text: string): string {
  return text.replace(/<br\s*\/?>/gi, "\n").replace(/<[^>]+>/g, "").replace(/#quot;/g, '"').trim();
}

/** Parse and check a sequence diagram before anything is written. */
export async function parseSequence(source: string): Promise<{ db: SequenceDb; decorations: Map<string, DiagramNode>; style?: ShapeStyle }> {
  let db: SequenceDb;
  try {
    db = (await parseMermaidText(source)).db as SequenceDb;
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new DiagramError([{ line: Number(/line (\d+)/i.exec(message)?.[1] ?? 0), message: `Mermaid could not parse this: ${message.split("\n").slice(0, 4).join(" ")}` }]);
  }
  const extras = source
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("%%") && !line.startsWith("%%{"))
    .map((line) => line.replace(/^%%\s?/, ""))
    .join("\n");
  const decorated = parseDiagram(extras);
  for (const node of decorated.nodes.values()) {
    if (!db.getActors().has(node.id)) {
      throw new DiagramError([{ line: 0, message: `the %% line for "${node.id}" names no participant of this diagram` }]);
    }
  }
  return { db, decorations: decorated.nodes, style: decorated.style };
}

/**
 * Draw a parsed sequence diagram into the room, replacing one with the same
 * id where it stands, or beside everything else if it is new.
 */
export function drawSequence(
  elements: Y.Map<NormalizedElement>,
  scene: Scene,
  source: string,
  parsed: { db: SequenceDb; decorations: Map<string, DiagramNode>; style?: ShapeStyle },
): SequenceResult {
  const { db, decorations } = parsed;
  const style: ShapeStyle = parsed.style ?? "sketch";
  const font = FONT[style];
  const roughness = style === "clean" ? 0 : 1;
  const L = db.LINETYPE;
  const title = plain(db.getDiagramTitle?.() ?? "");
  const id = slug(title || "sequence");
  const warnings: string[] = [];

  // Where it goes: where the last drawing of this id stood, or beside the rest.
  const origin = placeFixed(elements, scene, id);
  const tag = fixedTagger("sequence", id, style);
  const out: NormalizedElement[] = [];
  let z = -30;
  const push = (el: NormalizedElement) => {
    out.push({ ...el, z: z });
    z += 0.001;
  };

  // --- Participants -------------------------------------------------------
  const keys = db.getActorKeys();
  const actors = keys.map((key) => db.getActors().get(key) as Actor);
  // Room above the participants for the title, and for the labels of the
  // groups (Mermaid's `box`) that sit just above them.
  const titleY = (title ? 44 : 0) + (db.getBoxes().length > 0 ? 40 : 0);
  type Header = { actor: Actor; width: number; height: number; skeletons: ReturnType<typeof buildCard>["elements"] | null; title?: string };
  const headers: Header[] = actors.map((actor): Header => {
    const deco = decorations.get(actor.name);
    const [name, ...rest] = plain(actor.description ?? actor.name).split("\n");
    const kind = deco?.kind ?? (actor.type === "actor" ? "user" : actor.type === "database" ? "database" : "box");
    const card = CARD_KINDS.includes(kind) || (kind === "box" && deco?.icon);
    const titleText = deco?.title ?? name;
    const detail = deco?.detail ?? rest.join(" · ");
    if (card) {
      const built = buildCard({ kind, title: titleText, detail, style, badges: deco?.addBadges ?? [], color: deco?.color ?? undefined, icon: deco?.icon ?? undefined, node: actor.name });
      return { actor, width: built.width, height: built.height, skeletons: built.elements };
    }
    const width = Math.max(140, Math.ceil(measureText(titleText, 16).width) + 40);
    return { actor, width, height: CARD_HEIGHT, skeletons: null, title: titleText };
  });
  const colWidth = Math.max(...headers.map((h) => h.width)) + GAP;
  const centerX = (i: number) => origin.x + i * colWidth + colWidth / 2 - GAP / 2;
  const col = new Map(actors.map((a, i) => [a.name, i]));
  const top = origin.y + titleY;

  // --- Rows -----------------------------------------------------------------
  type Row = { y: number; height: number };
  let y = top + CARD_HEIGHT + 40;
  const nextRow = (height: number): Row => {
    const row = { y, height };
    y += height;
    return row;
  };

  const body: NormalizedElement[] = [];
  const frames: { kind: string; label: string; y: number; dividers: { y: number; label: string }[]; minCol: number; maxCol: number }[] = [];
  const openFrames: typeof frames = [];
  const activations = new Map<string, number[]>();
  const bars: { col: number; from: number; to: number; depth: number }[] = [];
  let numbering: { next: number; step: number; on: boolean } = { next: 1, step: 1, on: false };
  let messageCount = 0;

  const touch = (c: number) => {
    for (const f of openFrames) {
      f.minCol = Math.min(f.minCol, c);
      f.maxCol = Math.max(f.maxCol, c);
    }
  };

  for (const m of db.getMessages()) {
    const text = typeof m.message === "string" ? plain(m.message) : "";
    switch (m.type) {
      case L.AUTONUMBER: {
        const opts = typeof m.message === "object" && m.message ? m.message : {};
        numbering = { next: opts.start ?? 1, step: opts.step ?? 1, on: opts.visible !== false };
        continue;
      }
      case L.ACTIVE_START: {
        if (!m.from) continue;
        activations.set(m.from, [...(activations.get(m.from) ?? []), y - ROW / 2]);
        continue;
      }
      case L.ACTIVE_END: {
        if (!m.from) continue;
        const stack = activations.get(m.from) ?? [];
        const from = stack.pop();
        if (from !== undefined) bars.push({ col: col.get(m.from) ?? 0, from, to: y - ROW / 2 + 8, depth: stack.length });
        continue;
      }
      case L.LOOP_START:
      case L.ALT_START:
      case L.OPT_START:
      case L.PAR_START:
      case L.PAR_OVER_START:
      case L.CRITICAL_START:
      case L.BREAK_START:
      case L.RECT_START: {
        const kind =
          m.type === L.LOOP_START ? "loop" : m.type === L.ALT_START ? "alt" : m.type === L.OPT_START ? "opt"
          : m.type === L.CRITICAL_START ? "critical" : m.type === L.BREAK_START ? "break" : m.type === L.RECT_START ? "rect" : "par";
        const row = nextRow(BLOCK_ROW);
        const frame = { kind, label: kind === "rect" ? "" : text, y: row.y, dividers: [], minCol: Infinity, maxCol: -Infinity };
        frames.push(frame);
        openFrames.push(frame);
        continue;
      }
      case L.ALT_ELSE:
      case L.PAR_AND:
      case L.CRITICAL_OPTION: {
        const row = nextRow(BLOCK_ROW);
        openFrames[openFrames.length - 1]?.dividers.push({ y: row.y, label: text });
        continue;
      }
      case L.LOOP_END:
      case L.ALT_END:
      case L.OPT_END:
      case L.PAR_END:
      case L.CRITICAL_END:
      case L.BREAK_END:
      case L.RECT_END: {
        const frame = openFrames.pop();
        if (frame) (frame as { end?: number }).end = nextRow(14).y;
        continue;
      }
      case L.NOTE: {
        const a = col.get(m.from ?? "") ?? 0;
        const b = col.get(m.to ?? "") ?? a;
        touch(a);
        touch(b);
        const fields = boundTextFields(text, 15, NOTE_WIDTH);
        const height = Math.max(40, fields.height + 20);
        const row = nextRow(height + 16);
        const over = m.placement === db.PLACEMENT.OVER;
        const left = over ? Math.min(centerX(a), centerX(b)) - (a === b ? NOTE_WIDTH / 2 : 30) : m.placement === db.PLACEMENT.LEFTOF ? centerX(a) - NOTE_WIDTH - 16 : centerX(a) + 16;
        const width = over && a !== b ? Math.abs(centerX(b) - centerX(a)) + 60 : NOTE_WIDTH;
        const note = normalizeElement({
          type: "rectangle",
          x: left,
          y: row.y,
          width,
          height,
          backgroundColor: "#fff3bf",
          fillStyle: "solid",
          strokeColor: "#f08c00",
          strokeWidth: 1,
          roughness,
          customData: tag("note"),
        });
        const label = normalizeElement({
          type: "text",
          ...boundTextFields(text, 15, width),
          fontSize: 15,
          fontFamily: font,
          textAlign: "left",
          verticalAlign: "middle",
          containerId: note.id,
          x: left + 8,
          y: row.y + (height - fields.height) / 2,
          customData: tag("note-text"),
        });
        note.boundElements = [{ id: label.id, type: "text" }];
        body.push(note, label);
        continue;
      }
      default:
        break;
    }

    // A message.
    if (!m.from || !m.to) continue;
    const a = col.get(m.from);
    const b = col.get(m.to);
    if (a === undefined || b === undefined) continue;
    touch(a);
    touch(b);
    messageCount++;
    const number = numbering.on ? `${numbering.next}. ` : "";
    if (numbering.on) numbering.next += numbering.step;
    const words = number + text;
    const dotted = [L.DOTTED, L.DOTTED_CROSS, L.DOTTED_OPEN, L.DOTTED_POINT, L.BIDIRECTIONAL_DOTTED].includes(m.type);
    const head =
      [L.SOLID_OPEN, L.DOTTED_OPEN].includes(m.type) ? null
      : [L.SOLID_CROSS, L.DOTTED_CROSS].includes(m.type) ? "bar"
      : [L.SOLID_POINT, L.DOTTED_POINT].includes(m.type) ? "triangle_outline"
      : "arrow";
    const both = [L.BIDIRECTIONAL_SOLID, L.BIDIRECTIONAL_DOTTED].includes(m.type);
    const labelLines = boundTextFields(words, 15, 400);
    const row = nextRow(Math.max(ROW, labelLines.height + 34));
    const lineY = row.y + row.height - 14;
    const self = a === b;
    const x1 = centerX(a) + (self ? ACTIVATION_WIDTH / 2 : a < b ? ACTIVATION_WIDTH / 2 : -ACTIVATION_WIDTH / 2);
    const x2 = centerX(b) + (self ? ACTIVATION_WIDTH / 2 : a < b ? -ACTIVATION_WIDTH / 2 : ACTIVATION_WIDTH / 2);
    const points = self ? [[0, -18], [44, -18], [44, 6], [0, 6]] : [[0, 0], [x2 - x1, 0]];
    const arrow = normalizeElement({
      type: "arrow",
      x: x1,
      y: lineY + (self ? 0 : 0),
      points: self ? points.map(([px, py]) => [px, py + 18]) : points,
      width: self ? 44 : Math.abs(x2 - x1),
      height: self ? 24 : 0,
      strokeColor: "#1e1e1e",
      strokeStyle: dotted ? "dashed" : "solid",
      strokeWidth: 2,
      roughness,
      startArrowhead: both ? head : null,
      endArrowhead: head,
      customData: tag("message"),
    });
    if (self) arrow.y = lineY - 18;
    // The words sit above the line, not on it: a row is read left to right
    // along its arrow, and a label across the line hides where it starts.
    const textWidth = Math.min(labelLines.width, Math.max(Math.abs(x2 - x1) - 10, 120));
    const label = normalizeElement({
      type: "text",
      ...boundTextFields(words, 15, textWidth + 16),
      fontSize: 15,
      fontFamily: font,
      textAlign: self ? "left" : "center",
      verticalAlign: "top",
      x: self ? x1 + 52 : (x1 + x2) / 2 - textWidth / 2,
      y: self ? lineY - 18 : lineY - 8 - labelLines.height,
      customData: tag("message-text"),
    });
    body.push(arrow, label);
  }
  const bottom = y + 20;

  // --- Draw ---------------------------------------------------------------
  // Boxes (Mermaid's `box`): a zone around their participants' columns.
  for (const group of db.getBoxes()) {
    const cols = group.actorKeys.map((k) => col.get(k)).filter((c): c is number => c !== undefined);
    if (cols.length === 0) continue;
    const x = centerX(Math.min(...cols)) - colWidth / 2 + 12;
    const width = (Math.max(...cols) - Math.min(...cols) + 1) * colWidth - 24;
    for (const el of skeletonsToElements(
      buildZone({ kind: "network", title: plain(group.name ?? ""), width, height: bottom - top + 30, style }),
      { x, y: top - 34, z: -40 },
    )) out.push({ ...el, customData: tag("box") });
  }

  // Frames, outermost first so inner ones paint over them.
  frames.forEach((f, depth) => {
    const minCol = Number.isFinite(f.minCol) ? f.minCol : 0;
    const maxCol = Number.isFinite(f.maxCol) ? f.maxCol : actors.length - 1;
    const end = (f as { end?: number }).end ?? bottom;
    const inset = 6 * (depth % 4);
    const x = centerX(minCol) - colWidth / 2 + 20 + inset;
    const width = centerX(maxCol) - centerX(minCol) + colWidth - 40 - 2 * inset;
    push(normalizeElement({
      type: "rectangle",
      x,
      y: f.y,
      width,
      height: end - f.y,
      backgroundColor: f.kind === "rect" ? "#e7f5ff" : "transparent",
      fillStyle: "solid",
      strokeColor: f.kind === "rect" ? "#a5d8ff" : "#868e96",
      strokeWidth: 1,
      roughness,
      customData: tag("frame"),
    }));
    if (f.kind !== "rect") {
      push(normalizeElement({
        type: "text",
        text: f.label ? `${f.kind} [${f.label}]` : f.kind,
        fontSize: 14,
        fontFamily: font,
        strokeColor: "#495057",
        x: x + 8,
        y: f.y + 8,
        customData: tag("frame-label"),
      }));
    }
    for (const d of f.dividers) {
      push(normalizeElement({ type: "line", x, y: d.y + 4, points: [[0, 0], [width, 0]], width, height: 0, strokeColor: "#868e96", strokeStyle: "dashed", strokeWidth: 1, roughness, customData: tag("divider") }));
      if (d.label) push(normalizeElement({ type: "text", text: `[${d.label}]`, fontSize: 14, fontFamily: font, strokeColor: "#495057", x: x + 8, y: d.y + 10, customData: tag("divider-label") }));
    }
  });

  // Lifelines, each carrying the source.
  actors.forEach((_, i) => {
    push(normalizeElement({
      type: "line",
      x: centerX(i),
      y: top + CARD_HEIGHT,
      points: [[0, 0], [0, bottom - top - CARD_HEIGHT]],
      width: 0,
      height: bottom - top - CARD_HEIGHT,
      strokeColor: "#adb5bd",
      strokeStyle: "dashed",
      strokeWidth: 1,
      roughness: 0,
      customData: sourceTag(tag, "lifeline", source, origin),
    }));
  });

  // Activation bars, nested ones offset.
  for (const bar of bars) {
    push(normalizeElement({
      type: "rectangle",
      x: centerX(bar.col) - ACTIVATION_WIDTH / 2 + bar.depth * 6,
      y: bar.from,
      width: ACTIVATION_WIDTH,
      height: Math.max(16, bar.to - bar.from),
      backgroundColor: "#f1f3f5",
      fillStyle: "solid",
      strokeColor: "#868e96",
      strokeWidth: 1,
      roughness: 0,
      customData: tag("activation"),
    }));
  }

  for (const el of body) push(el);

  // Headers, on top of everything.
  headers.forEach((h, i) => {
    const x = centerX(i) - h.width / 2;
    if (h.skeletons) {
      for (const el of skeletonsToElements(h.skeletons, { x, y: top, z: 10 + i })) {
        const logos = (el.customData as { logos?: ShapeTag } | undefined)?.logos;
        out.push({ ...el, customData: { logos: { ...(logos as ShapeTag), seq: id } } });
      }
    } else {
      const frame = normalizeElement({
        type: "rectangle",
        x,
        y: top,
        width: h.width,
        height: CARD_HEIGHT,
        backgroundColor: "#ffffff",
        fillStyle: "solid",
        strokeColor: "#1e1e1e",
        strokeWidth: style === "clean" ? 1.5 : 2,
        roughness,
        roundness: { type: 3 },
        customData: tag("participant", { node: h.actor.name }),
        z: 10 + i,
      });
      const label = normalizeElement({
        type: "text",
        ...boundTextFields(h.title ?? h.actor.name, 16, h.width),
        fontSize: 16,
        fontFamily: font,
        textAlign: "center",
        verticalAlign: "middle",
        containerId: frame.id,
        x: x + 8,
        y: top + CARD_HEIGHT / 2 - 10,
        customData: tag("participant-text"),
        z: 10 + i,
      });
      frame.boundElements = [{ id: label.id, type: "text" }];
      out.push(frame, label);
    }
  });

  if (title) {
    push(normalizeElement({ type: "text", text: title, fontSize: 24, fontFamily: font, x: origin.x, y: origin.y, customData: tag("title") }));
  }

  for (const el of out) elements.set(el.id, el);
  return { id, participants: actors.length, messages: messageCount, warnings };
}

export type { Box };

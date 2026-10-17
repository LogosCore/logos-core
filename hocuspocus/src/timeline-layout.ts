// Mermaid timelines, drawn as Excalidraw shapes.
//
//   timeline
//     title Operation Nightjar
//     section Week 1 · Recon
//       3 Mar : Kick-off : scope signed
//       5 Mar : External scan
//     section Week 2 · Access
//       12 Mar : Foothold on WS-0142
//
// Periods run left to right along an axis, each a pill on the line; a
// period's events stack beneath it; sections are coloured bands above the
// periods they hold, and colour those periods' pills and events. Placement is
// fixed: a column per period, in the order written.

import type * as Y from "yjs";

import { COLORS, type ShapeStyle } from "./drawing-catalogue.js";
import { boundTextFields, measureText, normalizeElement, type NormalizedElement } from "./drawing-elements.js";
import { fixedTagger, placeFixed, sourceTag } from "./fixed-diagram.js";
import { commentExtras, plain, slug } from "./mermaid-common.js";
import type { Scene } from "./diagram-read.js";

interface TimelineTask {
  section: string;
  task: string;
  events: string[];
}
interface TimelineDb {
  getSections(): string[];
  getTasks(): TimelineTask[];
  getCommonDb?(): { getDiagramTitle?(): string };
}

const SECTION_COLORS = ["blue", "green", "orange", "violet", "red", "teal", "pink", "cyan", "lime", "grape"] as const;
const COLUMN_MIN = 170;
const COLUMN_MAX = 240;
const GAP = 26;
const EVENT_GAP = 12;
const FONT = { sketch: 5, clean: 6 } as const;

export interface TimelineResult {
  id: string;
  periods: number;
  events: number;
}

export function drawTimeline(
  elements: Y.Map<NormalizedElement>,
  scene: Scene,
  source: string,
  db: unknown,
): TimelineResult {
  const timeline = db as TimelineDb;
  const style: ShapeStyle = commentExtras(source).style ?? "sketch";
  const font = FONT[style];
  const roughness = style === "clean" ? 0 : 1;
  const title = plain(timeline.getCommonDb?.().getDiagramTitle?.() ?? "");
  const id = slug(title, "timeline");
  const origin = placeFixed(elements, scene, id);
  const tag = fixedTagger("timeline", id, style);

  const tasks = timeline.getTasks();
  const sectionNames = timeline.getSections();
  const colorOf = (section: string) => {
    const i = sectionNames.indexOf(section);
    return COLORS[SECTION_COLORS[(i < 0 ? 0 : i) % SECTION_COLORS.length]];
  };

  // Columns, as wide as their words need within bounds.
  const widths = tasks.map((t) => {
    const words = [plain(t.task), ...t.events.map(plain)];
    const widest = Math.max(...words.map((w) => measureText(w, 15, COLUMN_MAX - 24).width));
    return Math.min(COLUMN_MAX, Math.max(COLUMN_MIN, Math.ceil(widest) + 32));
  });
  const xs: number[] = [];
  let cursor = origin.x;
  for (const w of widths) {
    xs.push(cursor);
    cursor += w + GAP;
  }
  const right = cursor - GAP;

  const out: NormalizedElement[] = [];
  let z = -10;
  const add = (el: Record<string, unknown>) => {
    out.push(normalizeElement({ roughness, ...el, z: (z += 0.001) }));
    return out[out.length - 1];
  };
  const labelled = (box: Record<string, unknown>, text: string, fontSize: number, color: string, part: string) => {
    const shape = add(box);
    const fields = boundTextFields(text, fontSize, Number(box.width));
    const label = add({
      type: "text",
      ...fields,
      fontSize,
      fontFamily: font,
      strokeColor: color,
      textAlign: "center",
      verticalAlign: "middle",
      containerId: shape.id,
      x: Number(box.x) + (Number(box.width) - fields.width) / 2,
      y: Number(box.y) + (Number(box.height) - fields.height) / 2,
      customData: tag(`${part}-text`),
    });
    shape.boundElements = [{ id: label.id, type: "text" }];
  };

  let y = origin.y;
  if (title) {
    add({ type: "text", text: title, fontSize: 24, fontFamily: font, x: origin.x, y, customData: tag("title") });
    y += 48;
  }

  // Section bands, over the columns they hold.
  const bandY = y;
  const hasSections = sectionNames.length > 0;
  if (hasSections) {
    for (const section of sectionNames) {
      const cols = tasks.map((t, i) => (t.section === section ? i : -1)).filter((i) => i >= 0);
      if (cols.length === 0) continue;
      const x = xs[cols[0]];
      const width = xs[cols[cols.length - 1]] + widths[cols[cols.length - 1]] - x;
      const color = colorOf(section);
      labelled(
        { type: "rectangle", x, y: bandY, width, height: 34, backgroundColor: color.tile, fillStyle: "solid", strokeColor: color.border, strokeWidth: 1, roundness: { type: 3 }, customData: tag("section") },
        plain(section),
        15,
        color.ink,
        "section",
      );
    }
    y += 34 + 26;
  }

  // The axis, carrying the source.
  const axisY = y + 16;
  add({
    type: "arrow",
    x: origin.x - 10,
    y: axisY,
    points: [[0, 0], [right - origin.x + 30, 0]],
    width: right - origin.x + 30,
    height: 0,
    strokeColor: "#868e96",
    strokeWidth: 2,
    startArrowhead: null,
    endArrowhead: "triangle",
    customData: sourceTag(tag, "axis", source, origin),
  });

  let events = 0;
  tasks.forEach((t, i) => {
    const color = hasSections ? colorOf(t.section) : COLORS.blue;
    const x = xs[i];
    const w = widths[i];
    // The period: a pill on the line.
    const periodText = plain(t.task);
    const pillWidth = Math.min(w, Math.max(90, Math.ceil(measureText(periodText, 15).width) + 28));
    labelled(
      { type: "rectangle", x: x + (w - pillWidth) / 2, y: axisY - 16, width: pillWidth, height: 32, backgroundColor: color.ink === "#ffffff" ? "#343a40" : color.border, fillStyle: "solid", strokeColor: color.border, strokeWidth: 1, roundness: { type: 3 }, customData: tag("period") },
      periodText,
      15,
      "#ffffff",
      "period",
    );
    // Its events, stacked beneath.
    let ey = axisY + 40;
    const top = ey;
    for (const event of t.events.map(plain).filter(Boolean)) {
      const fields = boundTextFields(event, 15, w);
      const height = Math.max(40, fields.height + 20);
      labelled(
        { type: "rectangle", x, y: ey, width: w, height, backgroundColor: color.tile, fillStyle: "solid", strokeColor: color.border, strokeWidth: style === "clean" ? 1.5 : 2, roundness: { type: 3 }, customData: tag("event") },
        event,
        15,
        "#1e1e1e",
        "event",
      );
      ey += height + EVENT_GAP;
      events++;
    }
    if (ey > top) {
      add({
        type: "line",
        x: x + w / 2,
        y: axisY + 16,
        points: [[0, 0], [0, top - axisY - 16]],
        width: 0,
        height: top - axisY - 16,
        strokeColor: color.border,
        strokeWidth: 1,
        customData: tag("stem"),
      });
    }
  });

  for (const el of out) elements.set(el.id, el);
  return { id, periods: tasks.length, events };
}

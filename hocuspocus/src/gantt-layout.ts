// Mermaid Gantt charts, drawn as Excalidraw shapes.
//
//   gantt
//     title Engagement plan
//     dateFormat YYYY-MM-DD
//     section Recon
//     OSINT        :done, osint, 2026-03-03, 3d
//     Scan         :active, scan, after osint, 2d
//     section Access
//     Phishing     :crit, phish, 2026-03-10, 5d
//     Report       :milestone, rep, 2026-03-28, 0d
//
// Mermaid's parser has already resolved every date, "after" and duration, so
// what is left is a ruler: a date axis with ticks (days, weeks or months by
// span), a row per task under its section, a bar from start to end, and a
// diamond for a milestone. Bars take their section's colour; done is grey,
// active blue, critical red.

import type * as Y from "yjs";

import { COLORS, type ShapeStyle } from "./drawing-catalogue.js";
import { boundTextFields, measureText, normalizeElement, type NormalizedElement } from "./drawing-elements.js";
import { fixedTagger, placeFixed, sourceTag } from "./fixed-diagram.js";
import { commentExtras, plain, slug } from "./mermaid-common.js";
import type { Scene } from "./diagram-read.js";

interface GanttTask {
  id: string;
  task: string;
  section: string;
  startTime: Date;
  endTime: Date;
  milestone?: boolean;
  done?: boolean;
  active?: boolean;
  crit?: boolean;
}
interface GanttDb {
  getTasks(): GanttTask[];
  getSections(): string[];
  getDiagramTitle?(): string;
}

const DAY = 24 * 60 * 60 * 1000;
const ROW = 36;
const BAR = 24;
const SECTION_COLORS = ["blue", "green", "orange", "violet", "teal", "pink", "cyan", "lime", "grape"] as const;
const FONT = { sketch: 5, clean: 6 } as const;
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export interface GanttResult {
  id: string;
  tasks: number;
}

/** A calendar day as a number, whatever the time zone the parser used. */
function dayOf(d: Date): number {
  return Math.floor(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()) / DAY);
}

function dayLabel(day: number): string {
  const d = new Date(day * DAY);
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}

export function drawGantt(
  elements: Y.Map<NormalizedElement>,
  scene: Scene,
  source: string,
  db: unknown,
): GanttResult {
  const gantt = db as GanttDb;
  const style: ShapeStyle = commentExtras(source).style ?? "sketch";
  const font = FONT[style];
  const roughness = style === "clean" ? 0 : 1;
  const title = plain(gantt.getDiagramTitle?.() ?? "");
  const id = slug(title, "gantt");
  const origin = placeFixed(elements, scene, id);
  const tag = fixedTagger("gantt", id, style);

  const tasks = gantt.getTasks();
  const sections = [...new Set(tasks.map((t) => t.section))];
  const first = Math.min(...tasks.map((t) => dayOf(t.startTime)));
  // An end time is the start of the day after the last day, so it is the
  // day it lands on, except for a milestone, which has no length.
  const last = Math.max(...tasks.map((t) => (t.milestone ? dayOf(t.startTime) + 1 : Math.max(dayOf(t.endTime), dayOf(t.startTime) + 1))));
  const days = Math.max(1, last - first);
  const dayPx = Math.max(10, Math.min(56, 1100 / days));

  const out: NormalizedElement[] = [];
  let z = -10;
  const add = (el: Record<string, unknown>) => {
    out.push(normalizeElement({ roughness, ...el, z: (z += 0.001) }));
    return out[out.length - 1];
  };
  const text = (value: string, x: number, y: number, size: number, color: string, part: string, align = "left") =>
    add({ type: "text", text: value, fontSize: size, fontFamily: font, strokeColor: color, textAlign: align, x, y, customData: tag(part) });

  let y = origin.y;
  if (title) {
    text(title, origin.x, y, 24, "#1e1e1e", "title");
    y += 48;
  }

  // The label column, as wide as the longest task or section name.
  const labelWidth = Math.min(280, Math.max(140, ...tasks.map((t) => Math.ceil(measureText(plain(t.task), 15).width) + 24), ...sections.map((s) => Math.ceil(measureText(plain(s), 16).width) + 24)));
  const chartX = origin.x + labelWidth;
  const chartWidth = days * dayPx;
  const xOf = (day: number) => chartX + (day - first) * dayPx;

  // The axis, carrying the source, and its ticks.
  const axisY = y + 24;
  add({
    type: "line",
    x: chartX,
    y: axisY,
    points: [[0, 0], [chartWidth, 0]],
    width: chartWidth,
    height: 0,
    strokeColor: "#868e96",
    strokeWidth: 1.5,
    customData: sourceTag(tag, "axis", source, origin),
  });
  const rows = tasks.length + sections.length;
  const bottom = axisY + 12 + rows * ROW;
  const step = days <= 21 ? (dayPx >= 34 ? 1 : 2) : days <= 120 ? 7 : 30;
  for (let day = first; day <= last; day += step) {
    const x = xOf(day);
    text(dayLabel(day), x + 3, y, 12, "#868e96", "tick");
    add({ type: "line", x, y: axisY, points: [[0, 0], [0, bottom - axisY]], width: 0, height: bottom - axisY, strokeColor: "#e9ecef", strokeWidth: 1, roughness: 0, customData: tag("grid") });
  }

  // Sections and their tasks.
  let rowY = axisY + 12;
  sections.forEach((section, si) => {
    const color = COLORS[SECTION_COLORS[si % SECTION_COLORS.length]];
    const sectionTasks = tasks.filter((t) => t.section === section);
    // The band behind the section's rows.
    add({
      type: "rectangle",
      x: origin.x,
      y: rowY,
      width: labelWidth + chartWidth,
      height: ROW * (sectionTasks.length + 1),
      backgroundColor: color.tile,
      fillStyle: "solid",
      strokeColor: "transparent",
      opacity: 35,
      roughness: 0,
      customData: tag("band"),
    });
    if (section) text(plain(section), origin.x + 8, rowY + 9, 16, color.ink === "#ffffff" ? "#343a40" : color.ink, "section");
    rowY += ROW;
    for (const t of sectionTasks) {
      const name = plain(t.task);
      text(name, origin.x + 20, rowY + 9, 15, "#1e1e1e", "task");
      const start = dayOf(t.startTime);
      if (t.milestone) {
        const cx = xOf(start);
        add({ type: "diamond", x: cx - 11, y: rowY + ROW / 2 - 11, width: 22, height: 22, backgroundColor: "#343a40", fillStyle: "solid", strokeColor: "#343a40", strokeWidth: 1, customData: tag("milestone") });
        text(dayLabel(start), cx + 16, rowY + 10, 13, "#495057", "milestone-date");
      } else {
        const end = Math.max(dayOf(t.endTime), start + 1);
        const look = t.crit
          ? { fill: COLORS.red.tile, stroke: COLORS.red.border, width: 3 }
          : t.done
            ? { fill: "#dee2e6", stroke: "#868e96", width: 1.5 }
            : t.active
              ? { fill: COLORS.blue.tile, stroke: COLORS.blue.border, width: 3 }
              : { fill: color.tile, stroke: color.border, width: 1.5 };
        const bar = add({
          type: "rectangle",
          x: xOf(start),
          y: rowY + (ROW - BAR) / 2,
          width: (end - start) * dayPx,
          height: BAR,
          backgroundColor: look.fill,
          fillStyle: "solid",
          strokeColor: look.stroke,
          strokeWidth: look.width,
          roundness: { type: 3 },
          customData: tag("bar", { node: t.id }),
        });
        // The dates in the bar when they fit, beside it when they do not.
        const span = end - start > 1 ? `${dayLabel(start)} – ${dayLabel(end - 1)}` : dayLabel(start);
        const fields = boundTextFields(span, 12, Number(bar.width));
        if (measureText(span, 12).width + 12 <= Number(bar.width)) {
          const label = add({
            type: "text",
            ...fields,
            fontSize: 12,
            fontFamily: font,
            strokeColor: "#495057",
            textAlign: "center",
            verticalAlign: "middle",
            containerId: bar.id,
            x: Number(bar.x) + (Number(bar.width) - fields.width) / 2,
            y: Number(bar.y) + (BAR - fields.height) / 2,
            customData: tag("bar-text"),
          });
          bar.boundElements = [{ id: label.id, type: "text" }];
        } else {
          text(span, Number(bar.x) + Number(bar.width) + 6, rowY + 11, 12, "#495057", "bar-date");
        }
      }
      rowY += ROW;
    }
  });

  for (const el of out) elements.set(el.id, el);
  return { id, tasks: tasks.length };
}

// Draw the geometry of arrows bound to shapes: elbow, curved or straight.
//
// Used by both write paths: the diagram language (diagram-apply.ts), and
// edit_wiki_drawing for an element sent with `elbowed: true`. Each arrow is
// drawn the way its own fields say (elbowed, or roundness for a curve),
// between the shapes its bindings name, and its label moves to the middle of
// the new path. See arrow-routing.ts for the routes themselves.

import type * as Y from "yjs";

import {
  ELBOW_GAP,
  curveBetween,
  edgeToward,
  fixedPoint,
  routeElbow,
  spreadPorts,
  type Box,
  type ElbowRoute,
  type Point,
  type RoutedEnd,
} from "./arrow-routing.js";
import type { NormalizedElement } from "./drawing-elements.js";

export interface Obstacle {
  /** The element it stands for, so an arrow does not avoid its own ends. */
  id: string;
  box: Box;
}

function boxOf(el: NormalizedElement | undefined): Box | null {
  if (!el || el.isDeleted) return null;
  const box = { x: Number(el.x), y: Number(el.y), width: Number(el.width), height: Number(el.height) };
  return [box.x, box.y, box.width, box.height].every(Number.isFinite) ? box : null;
}

const endId = (arrow: NormalizedElement, end: "startBinding" | "endBinding") =>
  (arrow[end] as { elementId?: string } | null)?.elementId;

/** Lay out every arrow in `arrowIds` that has both ends bound. */
export function layoutArrows(
  elements: Y.Map<NormalizedElement>,
  arrowIds: readonly string[],
  obstacles: readonly Obstacle[],
): void {
  type Job = { id: string; arrow: NormalizedElement; fromId: string; toId: string; from: Box; to: Box };
  const jobs: Job[] = [];
  for (const id of new Set(arrowIds)) {
    const arrow = elements.get(id);
    if (!arrow || arrow.isDeleted) continue;
    const fromId = endId(arrow, "startBinding");
    const toId = endId(arrow, "endBinding");
    const from = fromId ? boxOf(elements.get(fromId)) : null;
    const to = toId ? boxOf(elements.get(toId)) : null;
    if (!fromId || !toId || !from || !to) continue;
    jobs.push({ id, arrow, fromId, toId, from, to });
  }

  const avoid = (job: Job) => obstacles.filter((o) => o.id !== job.fromId && o.id !== job.toId).map((o) => o.box);

  // Elbows: route each, then spread arrows sharing a side and route again
  // with their ends fixed where they were spread to.
  const elbows = jobs.filter((j) => j.arrow.elbowed === true);
  const first = elbows.map((job) => routeElbow({ from: job.from, to: job.to, obstacles: avoid(job) }));
  const ends: RoutedEnd[] = [];
  first.forEach((route, edge) => {
    const pts = route.points;
    ends.push({ edge, end: "from", node: elbows[edge].fromId, side: route.fromSide, toward: pts[Math.min(2, pts.length - 1)] });
    ends.push({ edge, end: "to", node: elbows[edge].toId, side: route.toSide, toward: pts[Math.max(pts.length - 3, 0)] });
  });
  const spread = spreadPorts(ends);
  elbows.forEach((job, edge) => {
    const fromAt = spread.get(`${edge}|from`) ?? 0.5;
    const toAt = spread.get(`${edge}|to`) ?? 0.5;
    const route =
      fromAt === 0.5 && toAt === 0.5
        ? first[edge]
        : routeElbow({
            from: job.from,
            to: job.to,
            obstacles: avoid(job),
            fromSide: first[edge].fromSide,
            toSide: first[edge].toSide,
            fromAt,
            toAt,
          });
    writeElbow(elements, job, route);
  });

  // Curves: several between the same two shapes bow apart.
  const lanes = new Map<string, number>();
  for (const job of jobs) {
    if (job.arrow.elbowed === true) continue;
    if (job.arrow.roundness) {
      const pair = [job.fromId, job.toId].sort().join("|");
      const lane = lanes.get(pair) ?? 0;
      lanes.set(pair, lane + 1);
      writePath(elements, job.id, curveBetween(job.from, job.to, lane), plainBindings(job.arrow));
    } else {
      const a = edgeToward(job.from, centerOf(job.to));
      const b = edgeToward(job.to, centerOf(job.from));
      writePath(elements, job.id, [a, b], plainBindings(job.arrow));
    }
  }
}

const centerOf = (b: Box): Point => [b.x + b.width / 2, b.y + b.height / 2];

/** Bindings without the elbow-only fixedPoint, for an arrow that is no
 * longer an elbow. */
function plainBindings(arrow: NormalizedElement): Record<string, unknown> {
  const plain = (b: unknown) => {
    if (!b || typeof b !== "object") return b ?? null;
    const { fixedPoint: _drop, ...rest } = b as Record<string, unknown>;
    return rest;
  };
  return { startBinding: plain(arrow.startBinding), endBinding: plain(arrow.endBinding), startIsSpecial: null, endIsSpecial: null };
}

function writeElbow(
  elements: Y.Map<NormalizedElement>,
  job: { id: string; from: Box; to: Box },
  route: ElbowRoute,
): void {
  const arrow = elements.get(job.id) as NormalizedElement;
  const binding = (end: "startBinding" | "endBinding", box: Box, side: ElbowRoute["fromSide"], at: number) => ({
    ...((arrow[end] as object | null) ?? {}),
    focus: 0,
    gap: ELBOW_GAP,
    fixedPoint: fixedPoint(box, side, at),
  });
  writePath(elements, job.id, route.points, {
    elbowed: true,
    roundness: null,
    fixedSegments: null,
    startIsSpecial: null,
    endIsSpecial: null,
    startBinding: binding("startBinding", job.from, route.fromSide, route.fromAt),
    endBinding: binding("endBinding", job.to, route.toSide, route.toAt),
  });
}

/** Store a path on an arrow, in Excalidraw's form: points relative to the
 * first, which sits at the arrow's x and y. */
function writePath(
  elements: Y.Map<NormalizedElement>,
  id: string,
  path: Point[],
  extra: Record<string, unknown> = {},
): void {
  const arrow = elements.get(id) as NormalizedElement;
  const [x0, y0] = path[0];
  const points = path.map(([x, y]) => [x - x0, y - y0] as Point);
  const xs = points.map((p) => p[0]);
  const ys = points.map((p) => p[1]);
  elements.set(id, {
    ...arrow,
    ...extra,
    x: x0,
    y: y0,
    points,
    width: Math.max(...xs) - Math.min(...xs),
    height: Math.max(...ys) - Math.min(...ys),
    version: (arrow.version ?? 1) + 1,
  });
  placeLabel(elements, id);
}

/**
 * Centre an arrow's label where Excalidraw puts it: on the middle point of an
 * odd number of points, else on the middle of the middle segment. The same
 * rule Excalidraw applies when a person moves the arrow, so the label does
 * not jump the first time somebody does.
 */
export function placeLabel(elements: Y.Map<NormalizedElement>, arrowId: string): void {
  const arrow = elements.get(arrowId);
  const points = arrow?.points as Point[] | undefined;
  if (!arrow || !Array.isArray(points) || points.length < 2) return;
  const bound = ((arrow.boundElements as { id: string; type: string }[] | null) ?? []).find((b) => b.type === "text");
  const label = bound ? elements.get(bound.id) : undefined;
  if (!label || label.isDeleted) return;
  let cx: number;
  let cy: number;
  if (points.length % 2 === 1) {
    [cx, cy] = points[(points.length - 1) / 2];
  } else {
    const a = points[points.length / 2 - 1];
    const b = points[points.length / 2];
    cx = (a[0] + b[0]) / 2;
    cy = (a[1] + b[1]) / 2;
  }
  elements.set(label.id, {
    ...label,
    x: Number(arrow.x) + cx - Number(label.width) / 2,
    y: Number(arrow.y) + cy - Number(label.height) / 2,
    version: (label.version ?? 1) + 1,
  });
}

/** Whether a path crosses a box, for finding arrows a new shape now sits on. */
export function pathCrosses(points: readonly Point[], origin: Point, box: Box): boolean {
  for (let i = 1; i < points.length; i++) {
    const ax = origin[0] + points[i - 1][0];
    const ay = origin[1] + points[i - 1][1];
    const bx = origin[0] + points[i][0];
    const by = origin[1] + points[i][1];
    for (let t = 0; t <= 1; t += 0.05) {
      const x = ax + (bx - ax) * t;
      const y = ay + (by - ay) * t;
      if (x > box.x && x < box.x + box.width && y > box.y && y < box.y + box.height) return true;
    }
  }
  return false;
}

export { ELBOW_GAP };

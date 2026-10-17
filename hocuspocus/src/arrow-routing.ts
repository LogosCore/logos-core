// Paths for arrows the server draws: elbow (horizontal and vertical only,
// around other shapes), curved, and straight.
//
// Excalidraw has all three as arrow types, but it routes an elbow arrow only
// in the browser, when somebody draws one or moves a shape it is attached to,
// and that router is not public API. An arrow written from outside keeps
// whatever points it was given. So the first route is drawn here; from then
// on Excalidraw re-routes it with its own router whenever a shape moves.
//
// The elbow router searches an orthogonal grid. The grid's lines are the
// edges of the nearby shapes, pushed out by a clearance, plus the lines
// through each arrow's ends; a path along them can go around a shape but
// never through one. The search is shortest-path with a penalty per bend, so
// it prefers one clean turn to a staircase. It also picks which side of each
// shape the arrow leaves from, preferring the sides that face each other.

export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

export type Point = [number, number];
export type Side = "left" | "right" | "top" | "bottom";

/** Space kept between a path and any shape it passes. */
const CLEARANCE = 20;
/** Excalidraw keeps an elbow arrow's tip this far off the shape
 * (FIXED_BINDING_DISTANCE in its source). */
export const ELBOW_GAP = 5;
/** What one bend costs, in pixels of extra length. */
const BEND_COST = 60;
/** What leaving from a side facing away from the target costs. */
const BACKWARD_SIDE_COST = 400;
/** Pull of the middle channel on where a turn happens, per pixel away. */
const TURN_PULL = 0.1;
/** How far around the two ends to look for shapes in the way. */
const SEARCH_MARGIN = 400;

const NORMALS: Record<Side, Point> = { left: [-1, 0], right: [1, 0], top: [0, -1], bottom: [0, 1] };
const SIDES: Side[] = ["left", "right", "top", "bottom"];

const center = (b: Box): Point => [b.x + b.width / 2, b.y + b.height / 2];

/** Where on a side an arrow attaches: `at` runs 0..1 along the side. */
export function sidePoint(box: Box, side: Side, at = 0.5): Point {
  switch (side) {
    case "left":
      return [box.x, box.y + box.height * at];
    case "right":
      return [box.x + box.width, box.y + box.height * at];
    case "top":
      return [box.x + box.width * at, box.y];
    case "bottom":
      return [box.x + box.width * at, box.y + box.height];
  }
}

/** The attachment as Excalidraw stores it on an elbow arrow: a fraction of
 * the shape's box. Exactly 0.5 is nudged, as Excalidraw itself does
 * (normalizeFixedPoint), because it reads an exact half as "no fixed point". */
export function fixedPoint(box: Box, side: Side, at = 0.5): Point {
  const [px, py] = sidePoint(box, side, at);
  const nudge = (r: number) => (Math.abs(r - 0.5) < 1e-4 ? 0.5001 : r);
  return [nudge((px - box.x) / box.width), nudge((py - box.y) / box.height)];
}

function inflate(b: Box, by: number): Box {
  return { x: b.x - by, y: b.y - by, width: b.width + 2 * by, height: b.height + 2 * by };
}

function strictlyInside(p: Point, b: Box): boolean {
  return p[0] > b.x + 1e-6 && p[0] < b.x + b.width - 1e-6 && p[1] > b.y + 1e-6 && p[1] < b.y + b.height - 1e-6;
}

function intersects(a: Box, b: Box): boolean {
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

export interface ElbowRequest {
  from: Box;
  to: Box;
  /** Every other shape the path should go around. */
  obstacles: readonly Box[];
  /** Fixed sides and positions along them, when already decided. */
  fromSide?: Side;
  toSide?: Side;
  fromAt?: number;
  toAt?: number;
}

export interface ElbowRoute {
  /** Scene coordinates, first point at the start tip, last at the end tip. */
  points: Point[];
  fromSide: Side;
  toSide: Side;
  fromAt: number;
  toAt: number;
}

/** A min-heap keyed on cost. */
class Heap<T> {
  private items: { cost: number; value: T }[] = [];
  push(cost: number, value: T) {
    const a = this.items;
    a.push({ cost, value });
    let i = a.length - 1;
    while (i > 0) {
      const p = (i - 1) >> 1;
      if (a[p].cost <= a[i].cost) break;
      [a[p], a[i]] = [a[i], a[p]];
      i = p;
    }
  }
  pop(): { cost: number; value: T } | undefined {
    const a = this.items;
    if (a.length === 0) return undefined;
    const top = a[0];
    const last = a.pop() as { cost: number; value: T };
    if (a.length > 0) {
      a[0] = last;
      let i = 0;
      for (;;) {
        const l = 2 * i + 1;
        const r = l + 1;
        let m = i;
        if (l < a.length && a[l].cost < a[m].cost) m = l;
        if (r < a.length && a[r].cost < a[m].cost) m = r;
        if (m === i) break;
        [a[m], a[i]] = [a[i], a[m]];
        i = m;
      }
    }
    return top;
  }
  get size() {
    return this.items.length;
  }
}

/** Drop points that sit on a straight run between their neighbours. */
function simplify(points: Point[]): Point[] {
  const out: Point[] = [];
  for (const p of points) {
    const last = out[out.length - 1];
    if (last && Math.abs(last[0] - p[0]) < 1e-6 && Math.abs(last[1] - p[1]) < 1e-6) continue;
    if (out.length >= 2) {
      const a = out[out.length - 2];
      const b = out[out.length - 1];
      const collinear = (Math.abs(a[0] - b[0]) < 1e-6 && Math.abs(b[0] - p[0]) < 1e-6) || (Math.abs(a[1] - b[1]) < 1e-6 && Math.abs(b[1] - p[1]) < 1e-6);
      if (collinear) {
        out[out.length - 1] = p;
        continue;
      }
    }
    out.push(p);
  }
  return out;
}

/**
 * Route an elbow arrow from one shape to another, around the obstacles.
 * Always returns a route: when no clear path exists, the best path that
 * ignores the obstacles.
 */
export function routeElbow(req: ElbowRequest): ElbowRoute {
  const region = inflate(
    {
      x: Math.min(req.from.x, req.to.x),
      y: Math.min(req.from.y, req.to.y),
      width: Math.max(req.from.x + req.from.width, req.to.x + req.to.width) - Math.min(req.from.x, req.to.x),
      height: Math.max(req.from.y + req.from.height, req.to.y + req.to.height) - Math.min(req.from.y, req.to.y),
    },
    SEARCH_MARGIN,
  );
  const blockers = [req.from, req.to, ...req.obstacles.filter((o) => intersects(o, region))].map((o) => inflate(o, CLEARANCE));
  return search(req, blockers) ?? search(req, [inflate(req.from, CLEARANCE), inflate(req.to, CLEARANCE)]) ?? straightElbow(req);
}

function search(req: ElbowRequest, blockers: Box[]): ElbowRoute | null {
  const target = center(req.to);
  const source = center(req.from);
  const fromSides = req.fromSide ? [req.fromSide] : SIDES;
  const toSides = req.toSide ? [req.toSide] : SIDES;

  // Ends: the tip on the shape, and a stub just clear of it.
  const starts = fromSides.map((side) => {
    const at = req.fromAt ?? 0.5;
    const [nx, ny] = NORMALS[side];
    const tip = sidePoint(req.from, side, at);
    const stub: Point = [tip[0] + nx * CLEARANCE, tip[1] + ny * CLEARANCE];
    const facing = (target[0] - tip[0]) * nx + (target[1] - tip[1]) * ny > 0;
    return { side, at, tip, stub, cost: facing ? 0 : BACKWARD_SIDE_COST };
  });
  const ends = toSides.map((side) => {
    const at = req.toAt ?? 0.5;
    const [nx, ny] = NORMALS[side];
    const tip = sidePoint(req.to, side, at);
    const stub: Point = [tip[0] + nx * CLEARANCE, tip[1] + ny * CLEARANCE];
    const facing = (source[0] - tip[0]) * nx + (source[1] - tip[1]) * ny > 0;
    return { side, at, tip, stub, cost: facing ? 0 : BACKWARD_SIDE_COST };
  });

  // Grid lines: blocker edges, the stubs, and the channel halfway between
  // the two shapes, which is where a single clean turn wants to be.
  const xsSet = new Set<number>();
  const ysSet = new Set<number>();
  for (const b of blockers) {
    xsSet.add(b.x);
    xsSet.add(b.x + b.width);
    ysSet.add(b.y);
    ysSet.add(b.y + b.height);
  }
  for (const e of [...starts, ...ends]) {
    xsSet.add(e.stub[0]);
    ysSet.add(e.stub[1]);
  }
  xsSet.add((source[0] + target[0]) / 2);
  ysSet.add((source[1] + target[1]) / 2);
  const xs = [...xsSet].sort((a, b) => a - b);
  const ys = [...ysSet].sort((a, b) => a - b);
  // Halfway lines between neighbouring grid lines: paths through the middle
  // of a gap rather than hugging one side of it.
  const mids = (v: number[]) => v.slice(1).map((n, i) => (n + v[i]) / 2);
  const gx = [...new Set([...xs, ...mids(xs)])].sort((a, b) => a - b);
  const gy = [...new Set([...ys, ...mids(ys)])].sort((a, b) => a - b);
  const xi = new Map(gx.map((v, i) => [v, i]));
  const yi = new Map(gy.map((v, i) => [v, i]));

  const free = (p: Point) => !blockers.some((b) => strictlyInside(p, b));
  const midX = (source[0] + target[0]) / 2;
  const midY = (source[1] + target[1]) / 2;
  const W = gx.length;
  const key = (i: number, j: number, d: number) => (j * W + i) * 4 + d;

  // dir: 0 right, 1 left, 2 down, 3 up.
  const steps: [number, number][] = [[1, 0], [-1, 0], [0, 1], [0, -1]];
  const dirOf = (side: Side) => ({ right: 0, left: 1, bottom: 2, top: 3 })[side];

  const best = new Map<number, number>();
  const prev = new Map<number, number>();
  const heap = new Heap<{ i: number; j: number; d: number }>();
  const startByKey = new Map<number, (typeof starts)[number]>();
  for (const s of starts) {
    const i = xi.get(s.stub[0]);
    const j = yi.get(s.stub[1]);
    if (i === undefined || j === undefined || !free(s.stub)) continue;
    const d = dirOf(s.side);
    const k = key(i, j, d);
    const cost = s.cost + CLEARANCE;
    if (cost < (best.get(k) ?? Infinity)) {
      best.set(k, cost);
      startByKey.set(k, s);
      heap.push(cost, { i, j, d });
    }
  }
  const endAt = new Map<number, (typeof ends)[number]>();
  for (const e of ends) {
    const i = xi.get(e.stub[0]);
    const j = yi.get(e.stub[1]);
    if (i !== undefined && j !== undefined) endAt.set(j * W + i, e);
  }

  let found: { k: number; end: (typeof ends)[number]; cost: number } | null = null;
  let expanded = 0;
  while (heap.size > 0 && expanded++ < 200_000) {
    const cur = heap.pop() as { cost: number; value: { i: number; j: number; d: number } };
    const { i, j, d } = cur.value;
    const k = key(i, j, d);
    if (cur.cost > (best.get(k) ?? Infinity)) continue;
    if (found && cur.cost >= found.cost) break;

    const end = endAt.get(j * W + i);
    if (end) {
      // Arriving must head into the shape; a last turn costs a bend.
      const into = dirOf(end.side) ^ 1;
      const total = cur.cost + end.cost + CLEARANCE + (d === into ? 0 : BEND_COST);
      if (!found || total < found.cost) found = { k, end, cost: total };
    }

    for (let nd = 0; nd < 4; nd++) {
      if (nd === (d ^ 1)) continue; // no U-turns
      const ni = i + steps[nd][0];
      const nj = j + steps[nd][1];
      if (ni < 0 || nj < 0 || ni >= W || nj >= gy.length) continue;
      const a: Point = [gx[i], gy[j]];
      const b: Point = [gx[ni], gy[nj]];
      if (!free(b) || !free([(a[0] + b[0]) / 2, (a[1] + b[1]) / 2])) continue;
      // A turn onto a vertical run prefers the channel halfway between the
      // shapes, and onto a horizontal run likewise: of two routes with the
      // same length and bends, the one that turns in the middle reads as a
      // diagram, the one that turns beside a shape reads as an accident.
      const turn = nd === d ? 0 : BEND_COST + TURN_PULL * (nd >= 2 ? Math.abs(a[0] - midX) : Math.abs(a[1] - midY));
      const cost = cur.cost + Math.abs(b[0] - a[0]) + Math.abs(b[1] - a[1]) + turn;
      const nk = key(ni, nj, nd);
      if (cost < (best.get(nk) ?? Infinity)) {
        best.set(nk, cost);
        prev.set(nk, k);
        heap.push(cost, { i: ni, j: nj, d: nd });
      }
    }
  }
  if (!found) return null;

  // Walk back to the stub the path began at.
  const grid: Point[] = [];
  let k: number | undefined = found.k;
  let first = found.k;
  while (k !== undefined) {
    const cell = Math.floor(k / 4);
    grid.push([gx[cell % W], gy[Math.floor(cell / W)]]);
    first = k;
    k = prev.get(k);
  }
  grid.reverse();
  const start = startByKey.get(first);
  if (!start) return null;
  const end = found.end;
  const gapped = (tip: Point, side: Side): Point => [tip[0] + NORMALS[side][0] * ELBOW_GAP, tip[1] + NORMALS[side][1] * ELBOW_GAP];
  return {
    points: simplify([gapped(start.tip, start.side), ...grid, gapped(end.tip, end.side)]),
    fromSide: start.side,
    toSide: end.side,
    fromAt: start.at,
    toAt: end.at,
  };
}

/** The last resort: facing sides and a Z through the middle. */
function straightElbow(req: ElbowRequest): ElbowRoute {
  const [sx, sy] = center(req.from);
  const [tx, ty] = center(req.to);
  const horizontal = Math.abs(tx - sx) >= Math.abs(ty - sy);
  const fromSide: Side = req.fromSide ?? (horizontal ? (tx > sx ? "right" : "left") : ty > sy ? "bottom" : "top");
  const toSide: Side = req.toSide ?? (horizontal ? (tx > sx ? "left" : "right") : ty > sy ? "top" : "bottom");
  const a = sidePoint(req.from, fromSide, req.fromAt ?? 0.5);
  const b = sidePoint(req.to, toSide, req.toAt ?? 0.5);
  const mid = horizontal ? (a[0] + b[0]) / 2 : (a[1] + b[1]) / 2;
  const points: Point[] = horizontal
    ? [a, [mid, a[1]], [mid, b[1]], b]
    : [a, [a[0], mid], [b[0], mid], b];
  return { points: simplify(points), fromSide, toSide, fromAt: req.fromAt ?? 0.5, toAt: req.toAt ?? 0.5 };
}

// ---------------------------------------------------------------------------
// Several arrows on one side

export interface RoutedEnd {
  /** Which arrow, and which of its ends. */
  edge: number;
  end: "from" | "to";
  node: string;
  side: Side;
  /** Where the arrow heads once it leaves the side: its next point. */
  toward: Point;
}

/**
 * Spread the arrows that share one side of one shape along it, in the order
 * they head off, so they leave side by side instead of from one point and
 * do not cross on the way out. Returns each end's position along its side.
 */
export function spreadPorts(ends: readonly RoutedEnd[]): Map<string, number> {
  const groups = new Map<string, RoutedEnd[]>();
  for (const e of ends) {
    const k = `${e.node}|${e.side}`;
    groups.set(k, [...(groups.get(k) ?? []), e]);
  }
  const out = new Map<string, number>();
  for (const group of groups.values()) {
    const along = (e: RoutedEnd) => (e.side === "left" || e.side === "right" ? e.toward[1] : e.toward[0]);
    group.sort((a, b) => along(a) - along(b));
    group.forEach((e, i) => out.set(`${e.edge}|${e.end}`, (i + 1) / (group.length + 1)));
  }
  return out;
}

// ---------------------------------------------------------------------------
// Curves

/**
 * A curved arrow between two shapes: the straight line, bowed by a point
 * off its middle. `lane` separates several curves between the same pair:
 * 0 bows one way, 1 the other, 2 further, and so on.
 */
export function curveBetween(from: Box, to: Box, lane = 0): Point[] {
  const a = edgeToward(from, center(to));
  const b = edgeToward(to, center(from));
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const length = Math.hypot(dx, dy) || 1;
  const sign = lane % 2 === 0 ? 1 : -1;
  const bow = Math.min(90, length * 0.18) * sign * (1 + Math.floor(lane / 2) * 0.8);
  const mid: Point = [a[0] + dx / 2 + (-dy / length) * bow, a[1] + dy / 2 + (dx / length) * bow];
  return [a, mid, b];
}

/** Where a ray from the box's centre towards a point leaves the box, plus a
 * small gap. */
export function edgeToward(box: Box, to: Point, gap = 4): Point {
  const [cx, cy] = center(box);
  const dx = to[0] - cx;
  const dy = to[1] - cy;
  if (dx === 0 && dy === 0) return [cx, cy];
  const scale = Math.min(dx === 0 ? Infinity : box.width / 2 / Math.abs(dx), dy === 0 ? Infinity : box.height / 2 / Math.abs(dy));
  const length = Math.hypot(dx, dy);
  const padded = scale + gap / length;
  return [cx + dx * padded, cy + dy * padded];
}

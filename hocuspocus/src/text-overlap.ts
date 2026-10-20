// Words drawn across other shapes.
//
// A label that runs over an arrow reads as struck through, and one that
// crosses a box's border reads as belonging to neither side — yet the write
// that put it there succeeded and said so. Nothing an agent can see in the
// response, or in an outline read, tells it the words collide: it would have
// to render the canvas. So the server looks, on the same geometry it stored.
//
// Only free text is checked. A bound label is laid out by Excalidraw inside
// its container, so where it was written is not where it is drawn.

import type { NormalizedElement } from "./drawing-elements.js";

interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface TextCollision {
  /** The free text that collides. */
  text: string;
  /** What it collides with. */
  other: string;
  kind: "arrow" | "text" | "border";
}

/** How far two shapes may touch before it counts: the measured boxes err
 * wide, and a label resting on its row's edge is not a collision. */
const SLACK = 2;

/** Where a free text is drawn, or null when its geometry is unusable. */
function textRect(el: NormalizedElement): Rect | null {
  const x = Number(el.x);
  const y = Number(el.y);
  let width = Number(el.width);
  let height = Number(el.height);
  if (!Number.isFinite(height) || height <= 0) {
    const lines = String(el.text ?? "").split("\n").length;
    height = lines * (Number(el.fontSize) || 20) * (Number(el.lineHeight) || 1.25);
  }
  if (!Number.isFinite(width) || width <= 0) width = 0;
  if (![x, y, width, height].every(Number.isFinite) || width === 0) return null;
  return { x: x + SLACK, y: y + SLACK, width: Math.max(0, width - 2 * SLACK), height: Math.max(0, height - 2 * SLACK) };
}

function shapeRect(el: NormalizedElement): Rect | null {
  const r = { x: Number(el.x), y: Number(el.y), width: Number(el.width), height: Number(el.height) };
  return Object.values(r).every(Number.isFinite) ? r : null;
}

function intersects(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

function contains(outer: Rect, inner: Rect): boolean {
  return (
    inner.x >= outer.x &&
    inner.y >= outer.y &&
    inner.x + inner.width <= outer.x + outer.width &&
    inner.y + inner.height <= outer.y + outer.height
  );
}

/** Whether the segment p→q passes through the rectangle (Liang–Barsky). */
function segmentHits(r: Rect, p: [number, number], q: [number, number]): boolean {
  const dx = q[0] - p[0];
  const dy = q[1] - p[1];
  let t0 = 0;
  let t1 = 1;
  const edges: [number, number][] = [
    [-dx, p[0] - r.x],
    [dx, r.x + r.width - p[0]],
    [-dy, p[1] - r.y],
    [dy, r.y + r.height - p[1]],
  ];
  for (const [pk, qk] of edges) {
    if (pk === 0) {
      if (qk < 0) return false;
      continue;
    }
    const t = qk / pk;
    if (pk < 0) t0 = Math.max(t0, t);
    else t1 = Math.min(t1, t);
    if (t0 > t1) return false;
  }
  return true;
}

function arrowHits(r: Rect, arrow: NormalizedElement): boolean {
  const points = arrow.points;
  if (!Array.isArray(points) || points.length < 2) return false;
  const ox = Number(arrow.x);
  const oy = Number(arrow.y);
  if (!Number.isFinite(ox) || !Number.isFinite(oy)) return false;
  const abs = (points as number[][]).map(([px, py]) => [ox + px, oy + py] as [number, number]);
  for (let i = 1; i < abs.length; i++) {
    if (segmentHits(r, abs[i - 1], abs[i])) return true;
  }
  return false;
}

const BOXES = new Set(["rectangle", "ellipse", "diamond", "frame", "image"]);

/**
 * Every free text that runs across an arrow, over another free text, or
 * across a box's border. A text wholly inside a box (a frame's caption) or
 * wholly clear of it is fine; one straddling its edge is not.
 *
 * With `onlyIDs`, only collisions involving at least one of those elements
 * are reported, so a write is told about what it did and not about a
 * collision that was already on the canvas.
 */
export function textCollisions(
  scene: Iterable<NormalizedElement | undefined>,
  onlyIDs?: ReadonlySet<string>,
): TextCollision[] {
  const live: NormalizedElement[] = [];
  for (const el of scene) if (el && !el.isDeleted) live.push(el);

  const texts: { el: NormalizedElement; rect: Rect }[] = [];
  for (const el of live) {
    if (el.type !== "text" || (typeof el.containerId === "string" && el.containerId !== "")) continue;
    const rect = textRect(el);
    if (rect) texts.push({ el, rect });
  }

  const relevant = (a: string, b: string) => !onlyIDs || onlyIDs.has(a) || onlyIDs.has(b);
  const out: TextCollision[] = [];

  for (const { el, rect } of texts) {
    for (const other of live) {
      if (other.id === el.id || !relevant(el.id, other.id)) continue;
      if (other.type === "arrow") {
        if (arrowHits(rect, other)) out.push({ text: el.id, other: other.id, kind: "arrow" });
      } else if (BOXES.has(other.type)) {
        const box = shapeRect(other);
        if (box && intersects(rect, box) && !contains(box, rect)) {
          out.push({ text: el.id, other: other.id, kind: "border" });
        }
      }
    }
  }

  // Each pair of texts once.
  for (let i = 0; i < texts.length; i++) {
    for (let j = i + 1; j < texts.length; j++) {
      const a = texts[i];
      const b = texts[j];
      if (!relevant(a.el.id, b.el.id)) continue;
      if (intersects(a.rect, b.rect)) out.push({ text: a.el.id, other: b.el.id, kind: "text" });
    }
  }
  return out;
}

/** The collisions as one warning for a write's response, or null. */
export function textCollisionWarning(collisions: readonly TextCollision[]): string | null {
  if (collisions.length === 0) return null;
  const say = { arrow: "runs across arrow", text: "overlaps text", border: "crosses the border of" } as const;
  const shown = collisions.slice(0, 5).map((c) => `text ${c.text} ${say[c.kind]} ${c.other}`);
  const more = collisions.length > shown.length ? `, and ${collisions.length - shown.length} more` : "";
  return (
    `${collisions.length} text placement${collisions.length === 1 ? "" : "s"} collide: ${shown.join("; ")}${more}. ` +
    `Words over a line read as struck through. Move the text clear, or give an arrow \`label\` ` +
    `and let it sit on the line.`
  );
}

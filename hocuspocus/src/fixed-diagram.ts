// What the fixed layouts share: sequence (sequence-layout.ts), timeline
// (timeline-layout.ts) and gantt (gantt-layout.ts).
//
// Unlike a graph, a fixed diagram is not merged node by node: it is drawn
// whole from its Mermaid, and drawing it again replaces it where it stands.
// Each part is tagged with the diagram's id (its title, else its type) and
// type, and one part keeps the source and where the diagram was drawn, which
// is how a read returns the source (diagram-read.ts) and a redraw finds the
// spot.

import type * as Y from "yjs";

import type { ShapeStyle, ShapeTag } from "./drawing-catalogue.js";
import type { NormalizedElement } from "./drawing-elements.js";
import type { Scene } from "./diagram-read.js";

export type FixedType = "sequence" | "timeline" | "gantt";

/** Where a fixed diagram goes, after taking down the one it replaces: its
 * stored origin, or beside everything else on the canvas. */
export function placeFixed(elements: Y.Map<NormalizedElement>, scene: Scene, id: string): { x: number; y: number } {
  const prior = scene.sequences.get(id);
  if (prior) {
    for (const elId of prior.elementIds) {
      const el = elements.get(elId);
      if (el && !el.isDeleted) elements.set(elId, { ...el, isDeleted: true, version: (el.version ?? 1) + 1, updated: Date.now() });
    }
    // Its own stored origin, not its outer box: the box takes in titles and
    // frames drawn above the origin, and starting from it would move the
    // diagram on every redraw.
    return prior.origin ?? { x: prior.box.x, y: prior.box.y };
  }
  const others = scene.bounds;
  return others ? { x: others.x + others.width + 160, y: others.y } : { x: 0, y: 0 };
}

/** Tags for a fixed diagram's parts. */
export function fixedTagger(type: FixedType, id: string, style: ShapeStyle) {
  return (part: string, extra: Partial<ShapeTag> & Record<string, unknown> = {}) => ({
    logos: { shape: `${type}-part`, part, seq: id, style, ...extra },
  });
}

/** Tags for the one part that keeps the source and the origin. */
export function sourceTag(
  tag: ReturnType<typeof fixedTagger>,
  part: string,
  source: string,
  origin: { x: number; y: number },
) {
  return tag(part, { source, origin: [origin.x, origin.y] });
}

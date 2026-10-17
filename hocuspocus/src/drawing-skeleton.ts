// Catalogue shapes, made into real elements on the server.
//
// The catalogue (drawing-catalogue.ts) describes shapes as Excalidraw
// skeletons, the builder input the canvas converts with Excalidraw's own
// convertToExcalidrawElements. That function needs a browser. This does the
// same job with the sidecar's element normaliser, for the shapes the
// catalogue actually produces: rectangles, ellipses, lines and texts, and
// shapes carrying a `label`.
//
// Every call gets fresh element ids and fresh group ids. The catalogue uses
// one fixed group id per shape ("card-server"), so two server cards built
// from it would otherwise be one group, and select and move together.

import type { Skeleton } from "./drawing-catalogue.js";
import { measureText, normalizeElement, randomID, type NormalizedElement } from "./drawing-elements.js";

export interface BuildOptions {
  /** Where the shape's origin goes. */
  x: number;
  y: number;
  /** Id for the first element, the shape's frame. A rebuilt card keeps its
   * frame's id, so the arrows bound to it stay bound. */
  frameId?: string;
  /** Layer for every element; see drawing-order.ts. */
  z?: number;
}

export function skeletonsToElements(skeletons: readonly Skeleton[], opts: BuildOptions): NormalizedElement[] {
  const groups = new Map<string, string>();
  const group = (id: string) => {
    if (!groups.has(id)) groups.set(id, randomID());
    return groups.get(id) as string;
  };
  const regroup = (ids: unknown) => (Array.isArray(ids) ? ids.map((id) => group(String(id))) : []);
  const z = opts.z !== undefined ? { z: opts.z } : {};

  const out: NormalizedElement[] = [];
  skeletons.forEach((skeleton, i) => {
    const { label, ...fields } = skeleton;
    const element = normalizeElement({
      ...fields,
      ...(i === 0 && opts.frameId ? { id: opts.frameId } : {}),
      x: skeleton.x + opts.x,
      y: skeleton.y + opts.y,
      groupIds: regroup(skeleton.groupIds),
      // Catalogue text is laid out from its top-left, like Excalidraw's
      // own free text.
      ...(skeleton.type === "text" ? { textAlign: skeleton.textAlign ?? "left", verticalAlign: "top" } : {}),
      ...z,
    });
    out.push(element);

    if (label && typeof label === "object") {
      const text = boundLabel(element, label as Record<string, unknown>, regroup);
      element.boundElements = [...((element.boundElements as unknown[]) ?? []), { id: text.id, type: "text" }];
      out.push({ ...text, ...z });
    }
  });
  return out;
}

/** A label inside its shape, centred unless the label asks otherwise. */
function boundLabel(
  container: NormalizedElement,
  label: Record<string, unknown>,
  regroup: (ids: unknown) => string[],
): NormalizedElement {
  const text = String(label.text ?? "");
  const fontSize = typeof label.fontSize === "number" ? label.fontSize : 16;
  const textAlign = typeof label.textAlign === "string" ? label.textAlign : "center";
  const verticalAlign = typeof label.verticalAlign === "string" ? label.verticalAlign : "middle";
  const box = {
    x: Number(container.x),
    y: Number(container.y),
    width: Number(container.width),
    height: Number(container.height),
  };
  const pad = 8;
  const measured = measureText(text, fontSize, Math.max(box.width - 2 * pad, 16));
  const width = Math.min(Math.ceil(measured.width), Math.max(box.width - 2 * pad, 16));
  return normalizeElement({
    type: "text",
    text,
    fontSize,
    fontFamily: label.fontFamily,
    strokeColor: label.strokeColor ?? container.strokeColor,
    textAlign,
    verticalAlign,
    containerId: container.id,
    groupIds: label.groupIds !== undefined ? regroup(label.groupIds) : container.groupIds,
    customData: label.customData,
    // Excalidraw re-lays a bound label out on load; these are a sane
    // starting box so nothing jumps on first render.
    x: textAlign === "left" ? box.x + pad : box.x + (box.width - width) / 2,
    y: verticalAlign === "top" ? box.y + pad : box.y + (box.height - measured.height) / 2,
    width,
    height: Math.ceil(measured.height),
  });
}

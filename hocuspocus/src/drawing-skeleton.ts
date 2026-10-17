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
import { boundTextFields, normalizeElement, randomID, type NormalizedElement } from "./drawing-elements.js";

export interface BuildOptions {
  /** Where the shape's origin goes. */
  x: number;
  y: number;
  /** Id for the first element, the shape's frame. A rebuilt card keeps its
   * frame's id, so the arrows bound to it stay bound. */
  frameId?: string;
  /** Layer for the shape; see drawing-order.ts. Each part is placed a
   * hair above the one before it, so the shape paints in the order the
   * catalogue lists its parts. */
  z?: number;
}

export function skeletonsToElements(skeletons: readonly Skeleton[], opts: BuildOptions): NormalizedElement[] {
  const groups = new Map<string, string>();
  const group = (id: string) => {
    if (!groups.has(id)) groups.set(id, randomID());
    return groups.get(id) as string;
  };
  const regroup = (ids: unknown) => (Array.isArray(ids) ? ids.map((id) => group(String(id))) : []);
  // Paint order is z, then Excalidraw's fractional index, then element id
  // (drawing-order.ts). Elements written here have no index, so parts on one
  // z were painted in id order, which is random: a card's white frame could
  // cover its own title and icon. A step per part keeps the catalogue's
  // order; it is far below 1, the gap between layers.
  const PART_STEP = 1e-4;
  let part = 0;
  const z = () => (opts.z !== undefined ? { z: opts.z + PART_STEP * part++ } : {});

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
      ...z(),
    });
    out.push(element);

    if (label && typeof label === "object") {
      const text = boundLabel(element, label as Record<string, unknown>, regroup);
      element.boundElements = [...((element.boundElements as unknown[]) ?? []), { id: text.id, type: "text" }];
      out.push({ ...text, ...z() });
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
  const fields = boundTextFields(text, fontSize, box.width);
  const width = fields.width;
  return normalizeElement({
    type: "text",
    text: fields.text,
    originalText: fields.originalText,
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
    y: verticalAlign === "top" ? box.y + pad : box.y + (box.height - fields.height) / 2,
    width,
    height: fields.height,
  });
}

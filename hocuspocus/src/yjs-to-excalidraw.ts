// Read a drawing page's scene out of its Y.js binary state.
//
// The prose counterpart is yjs-to-markdown.ts. Both exist for the same
// reason: content_state is written by this service and only this service can
// read it back, so the Go export flow asks us rather than parsing CRDT bytes
// of its own. A drawing has no markdown, and used to leave a wiki export as a
// line in the report saying so; this is what lets it leave as a drawing.
//
// What comes back is the scene, not a file: elements in paint order and the
// shared slice of appState. The exporter wraps it in Excalidraw's file
// envelope and fills in `files` — the image bytes live in the blob store,
// which is the Go side's to read, not ours.

import * as Y from "yjs";
import { DRAWING_ROOT_KEY } from "./drawing-projection.js";
import { inPaintOrder } from "./drawing-order.js";

/** Root key holding the shared slice of Excalidraw's appState. Mirrors
 * DRAWING_APP_STATE_KEY in frontend/src/components/wiki/drawing/drawing-scene.ts. */
export const DRAWING_APP_STATE_KEY = "excalidraw:appState";

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** An element, in the shape this module reads. Everything else on it is
 * passed through untouched — an element carries about twenty-five fields and
 * naming them here would mean dropping whichever one Excalidraw adds next. */
interface SceneElement {
  id?: string;
  type?: string;
  isDeleted?: boolean;
  fileId?: string;
  z?: unknown;
  index?: unknown;
}

export interface ExcalidrawScene {
  /** Live elements, back to front. */
  elements: SceneElement[];
  /** The page-level slice of appState: background colour, grid. */
  appState: Record<string, unknown>;
  /** Wiki image ids the scene's image elements point at. */
  imageIds: string[];
}

export const EMPTY_SCENE: ExcalidrawScene = {
  elements: [],
  appState: {},
  imageIds: [],
};

/**
 * Convert a Y.js update binary into a scene.
 *
 * Deleted elements are dropped rather than exported as tombstones. A live
 * room keeps them so peers learn about erasures, but a file is not a room:
 * an exported scene has no peers to tell, and Excalidraw would carry the
 * tombstones forward into every copy anyone made of it.
 *
 * Throws if the update bytes are not a valid Y.js update; the caller treats
 * that as a per-document failure, not the end of the export.
 */
export function yjsUpdateToExcalidrawScene(update: Uint8Array): ExcalidrawScene {
  const ydoc = new Y.Doc();
  try {
    Y.applyUpdate(ydoc, update);

    const stored = ydoc.getMap<SceneElement>(DRAWING_ROOT_KEY);
    if (stored.size === 0) return EMPTY_SCENE;

    const live = [...stored.values()].filter(
      (el): el is SceneElement =>
        typeof el === "object" && el !== null && !el.isDeleted,
    );

    const elements = inPaintOrder(
      live.map((el) => ({ ...el, id: typeof el.id === "string" ? el.id : "" })),
    );

    const imageIds = new Set<string>();
    for (const el of elements) {
      // Only ids that are ours: an image the editor never adopted carries the
      // id Excalidraw minted for it and points at no blob to embed.
      if (
        el.type === "image" &&
        typeof el.fileId === "string" &&
        UUID_RE.test(el.fileId)
      ) {
        imageIds.add(el.fileId.toLowerCase());
      }
    }

    return {
      elements,
      appState: readSharedAppState(ydoc),
      imageIds: [...imageIds],
    };
  } finally {
    ydoc.destroy();
  }
}

/** The page-level appState, with the per-viewer fields left out — the same
 * subset the canvas shares between collaborators. */
function readSharedAppState(ydoc: Y.Doc): Record<string, unknown> {
  const stored = ydoc.getMap(DRAWING_APP_STATE_KEY);
  const state: Record<string, unknown> = {};
  const background = stored.get("viewBackgroundColor");
  if (typeof background === "string") state.viewBackgroundColor = background;
  if (stored.has("gridSize")) state.gridSize = stored.get("gridSize");
  return state;
}

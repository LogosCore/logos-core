import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import {
  DRAWING_APP_STATE_KEY,
  yjsUpdateToExcalidrawScene,
} from "../yjs-to-excalidraw.js";
import { DRAWING_ROOT_KEY } from "../drawing-projection.js";

/** A Y.js update holding the given elements and shared appState, as the
 * canvas would have written them. */
function sceneUpdate(
  elements: Record<string, unknown>[],
  appState: Record<string, unknown> = {},
): Uint8Array {
  const ydoc = new Y.Doc();
  const map = ydoc.getMap(DRAWING_ROOT_KEY);
  const state = ydoc.getMap(DRAWING_APP_STATE_KEY);
  ydoc.transact(() => {
    for (const el of elements) map.set(el.id as string, el);
    for (const [k, v] of Object.entries(appState)) state.set(k, v);
  });
  const update = Y.encodeStateAsUpdate(ydoc);
  ydoc.destroy();
  return update;
}

test("elements come back in paint order, not map order", () => {
  const scene = yjsUpdateToExcalidrawScene(
    sceneUpdate([
      { id: "front", type: "rectangle", index: "a2" },
      { id: "behind", type: "rectangle", z: -1 },
      { id: "back", type: "rectangle", index: "a1" },
    ]),
  );
  assert.deepEqual(
    scene.elements.map((e) => e.id),
    ["behind", "back", "front"],
  );
});

// A live room keeps erased elements so peers learn about the erasure. A file
// has no peers, and carrying the tombstones forward would put them in every
// copy anyone made of the drawing.
test("deleted elements do not leave the room", () => {
  const scene = yjsUpdateToExcalidrawScene(
    sceneUpdate([
      { id: "kept", type: "rectangle" },
      { id: "rubbed-out", type: "rectangle", isDeleted: true },
    ]),
  );
  assert.deepEqual(
    scene.elements.map((e) => e.id),
    ["kept"],
  );
});

test("the page's own appState travels, the viewer's does not", () => {
  const scene = yjsUpdateToExcalidrawScene(
    sceneUpdate([{ id: "a", type: "rectangle" }], {
      viewBackgroundColor: "#1e1e1e",
      gridSize: 20,
    }),
  );
  assert.deepEqual(scene.appState, {
    viewBackgroundColor: "#1e1e1e",
    gridSize: 20,
  });
});

test("only adopted images are listed for embedding", () => {
  const wikiImage = "3f2504e0-4f89-11d3-9a0c-0305e82c3301";
  const scene = yjsUpdateToExcalidrawScene(
    sceneUpdate([
      { id: "a", type: "image", fileId: wikiImage },
      // Excalidraw's own id: the upload never completed, so there is no blob.
      { id: "b", type: "image", fileId: "8f1cd2ab99" },
      { id: "c", type: "image", fileId: wikiImage.toUpperCase() },
    ]),
  );
  assert.deepEqual(scene.imageIds, [wikiImage]);
});

test("an untouched drawing is an empty scene, not a failure", () => {
  const ydoc = new Y.Doc();
  const update = Y.encodeStateAsUpdate(ydoc);
  ydoc.destroy();

  const scene = yjsUpdateToExcalidrawScene(update);
  assert.deepEqual(scene.elements, []);
  assert.deepEqual(scene.imageIds, []);
});

test("bytes that are not a Y.js update throw, for the caller to record", () => {
  assert.throws(() =>
    yjsUpdateToExcalidrawScene(new Uint8Array([1, 2, 3, 4, 5])),
  );
});

// The full shape the Go exporter decodes. A field renamed here and not there
// is an export that silently produces empty scenes.
test("the response carries elements, appState and imageIds", () => {
  const scene = yjsUpdateToExcalidrawScene(
    sceneUpdate([{ id: "a", type: "rectangle", x: 10, y: 20 }]),
  );
  assert.deepEqual(Object.keys(scene).sort(), [
    "appState",
    "elements",
    "imageIds",
  ]);
  // Element fields this module knows nothing about have to survive.
  assert.equal((scene.elements[0] as { x?: number }).x, 10);
});

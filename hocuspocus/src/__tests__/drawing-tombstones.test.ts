import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { DRAWING_ROOT_KEY } from "../drawing-projection.js";
import {
  TOMBSTONE_MAX_AGE_MS,
  pruneDrawingTombstones,
  pruneStoredDrawing,
} from "../drawing-tombstones.js";

const NOW = Date.UTC(2026, 9, 17);
const OLD = NOW - TOMBSTONE_MAX_AGE_MS - 1;
const RECENT = NOW - 60_000;

type El = Record<string, unknown> & { id: string };

function elements(doc: Y.Doc): Y.Map<El> {
  return doc.getMap<El>(DRAWING_ROOT_KEY);
}

/** A long freehand stroke, which is what makes a tombstone expensive. */
function stroke(id: string, extra: Partial<El> = {}): El {
  const points = Array.from({ length: 300 }, (_, i) => [i * 1.37, Math.sin(i / 7) * 60]);
  return { id, type: "freedraw", version: 300, versionNonce: 7, points, ...extra };
}

function scene(): Y.Doc {
  const doc = new Y.Doc();
  const map = elements(doc);
  map.set("live", { id: "live", type: "rectangle", version: 3, updated: OLD });
  map.set("old", stroke("old", { isDeleted: true, updated: OLD }));
  map.set("recent", stroke("recent", { isDeleted: true, updated: RECENT }));
  map.set("undated", stroke("undated", { isDeleted: true }));
  map.set("future", stroke("future", { isDeleted: true, updated: NOW + 60_000 }));
  return doc;
}

test("only tombstones older than the window are removed", () => {
  const doc = scene();

  assert.equal(pruneDrawingTombstones(doc, NOW), 1);

  assert.deepEqual([...elements(doc).keys()].sort(), ["future", "live", "recent", "undated"]);
});

// A shape that is still on the canvas is never touched, however long ago it
// was last changed. Age only matters for something already erased.
test("a live shape is kept whatever its age", () => {
  const doc = new Y.Doc();
  elements(doc).set("a", { id: "a", version: 1, updated: 0 });

  assert.equal(pruneDrawingTombstones(doc, NOW), 0);
  assert.equal(elements(doc).has("a"), true);
});

test("the stored state shrinks by the erased stroke", () => {
  const state = Y.encodeStateAsUpdate(scene());

  const { state: pruned, removed } = pruneStoredDrawing(state, NOW);

  assert.equal(removed, 1);
  assert.ok(pruned.length < state.length - 3000, `${state.length} -> ${pruned.length}`);
});

test("a state with nothing to remove is returned as it was", () => {
  const doc = new Y.Doc();
  elements(doc).set("recent", stroke("recent", { isDeleted: true, updated: RECENT }));
  const state = Y.encodeStateAsUpdate(doc);

  const result = pruneStoredDrawing(state, NOW);

  assert.equal(result.removed, 0);
  assert.equal(result.state, state);
});

// The reason this can run at all. A browser that had the page open still
// holds the tombstone; one that missed the erase holds the live shape. Both
// reconnect and sync their state into the room, and neither may bring the
// key back.
test("a browser reconnecting with the old state does not bring the shape back", () => {
  const before = new Y.Doc();
  elements(before).set("old", stroke("old", { version: 1 }));
  const missedErase = new Y.Doc();
  Y.applyUpdate(missedErase, Y.encodeStateAsUpdate(before));

  elements(before).set("old", stroke("old", { version: 2, isDeleted: true, updated: OLD }));
  const sawErase = new Y.Doc();
  Y.applyUpdate(sawErase, Y.encodeStateAsUpdate(before));

  const room = new Y.Doc();
  Y.applyUpdate(room, pruneStoredDrawing(Y.encodeStateAsUpdate(before), NOW).state);
  Y.applyUpdate(room, Y.encodeStateAsUpdate(sawErase));
  Y.applyUpdate(room, Y.encodeStateAsUpdate(missedErase));

  assert.equal(elements(room).has("old"), false);
});

// What the browser sees: the removal arrives as a key deletion, with the
// value the room held. The frontend hides the shape on that signal.
test("a browser holding the shape is told the key was removed", () => {
  const doc = new Y.Doc();
  elements(doc).set("old", stroke("old", { isDeleted: true, updated: OLD }));
  const browser = new Y.Doc();
  Y.applyUpdate(browser, Y.encodeStateAsUpdate(doc));
  const deleted: string[] = [];
  elements(browser).observe((event) => {
    for (const [id, change] of event.changes.keys) if (change.action === "delete") deleted.push(id);
  });

  const pruned = pruneStoredDrawing(Y.encodeStateAsUpdate(doc), NOW).state;
  Y.applyUpdate(browser, pruned);

  assert.deepEqual(deleted, ["old"]);
});

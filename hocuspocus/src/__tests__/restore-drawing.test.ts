import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { DRAWING_ROOT_KEY } from "../drawing-projection.js";
import { DRAWING_APP_STATE_KEY } from "../yjs-to-excalidraw.js";
import { restoreDrawingInto } from "../restore-drawing.js";

type SceneElement = Record<string, unknown> & { id: string; version: number; versionNonce?: number };

function elements(doc: Y.Doc): Y.Map<SceneElement> {
  return doc.getMap<SceneElement>(DRAWING_ROOT_KEY);
}

function live(doc: Y.Doc): string[] {
  return [...elements(doc).values()]
    .filter((el) => el.isDeleted !== true)
    .map((el) => el.id)
    .sort();
}

/** A room as a backup snapshot saw it, then edited past that point. */
function editedSinceBackup(): { room: Y.Doc; backup: Uint8Array } {
  const room = new Y.Doc();
  const map = elements(room);
  map.set("box", { id: "box", type: "rectangle", x: 0, version: 2, versionNonce: 10 });
  map.set("gone", { id: "gone", type: "ellipse", version: 1, versionNonce: 11 });
  room.getMap(DRAWING_APP_STATE_KEY).set("viewBackgroundColor", "#ffffff");
  const backup = Y.encodeStateAsUpdate(room);

  // After the snapshot: the box moves, a shape is erased, a new one is drawn,
  // and the background changes.
  map.set("box", { id: "box", type: "rectangle", x: 500, version: 7, versionNonce: 20 });
  map.set("gone", { id: "gone", type: "ellipse", version: 2, versionNonce: 21, isDeleted: true });
  map.set("new", { id: "new", type: "diamond", version: 1, versionNonce: 22 });
  room.getMap(DRAWING_APP_STATE_KEY).set("viewBackgroundColor", "#000000");
  return { room, backup };
}

// The reason the restore cannot be a byte write over content_state. A browser
// that had the page open reconnects holding the edited state, Y.js merges it
// into the restored bytes, and everything drawn after the backup is back.
test("writing the backup bytes over the row is undone by a reconnecting browser", () => {
  const { room, backup } = editedSinceBackup();

  const restoredRow = new Y.Doc();
  Y.applyUpdate(restoredRow, backup);
  // The browser's copy is the room as it stood; reconnecting syncs it in.
  Y.applyUpdate(restoredRow, Y.encodeStateAsUpdate(room));

  assert.deepEqual(live(restoredRow), ["box", "new"]);
  assert.equal(elements(restoredRow).get("box")?.x, 500);
});

test("the room ends up holding exactly the backup's scene", () => {
  const { room, backup } = editedSinceBackup();

  const result = room.transact(() => restoreDrawingInto(room, backup));

  assert.deepEqual(live(room), ["box", "gone"]);
  assert.equal(elements(room).get("box")?.x, 0);
  assert.equal(elements(room).get("new")?.isDeleted, true);
  assert.equal(room.getMap(DRAWING_APP_STATE_KEY).get("viewBackgroundColor"), "#ffffff");
  assert.deepEqual(result, { restored: 2, removed: 1 });
});

// A canvas takes an element from the room only when its version is above the
// one it holds (shouldKeepLocal in the frontend's drawing-scene.ts). The
// backup's copies are older, so written back at their own versions every open
// canvas would keep what it has and the restore would show nowhere.
test("every restored element outranks the copy a connected canvas holds", () => {
  const { room, backup } = editedSinceBackup();
  const before = new Map([...elements(room).entries()].map(([id, el]) => [id, el.version]));

  room.transact(() => restoreDrawingInto(room, backup));

  for (const [id, el] of elements(room).entries()) {
    assert.ok(el.version > (before.get(id) ?? 0), `${id} is at ${el.version}, not above ${before.get(id)}`);
  }
});

// And the restore, unlike the byte write, survives the same reconnect: the
// browser's older copies lose to the restored versions in the CRDT too.
test("a browser reconnecting with the pre-restore state does not undo it", () => {
  const { room, backup } = editedSinceBackup();
  const browser = new Y.Doc();
  Y.applyUpdate(browser, Y.encodeStateAsUpdate(room));

  room.transact(() => restoreDrawingInto(room, backup));
  Y.applyUpdate(room, Y.encodeStateAsUpdate(browser));

  assert.deepEqual(live(room), ["box", "gone"]);
  assert.equal(elements(room).get("box")?.x, 0);
});

test("a background chosen after the backup is cleared, not kept", () => {
  const room = new Y.Doc();
  const backup = Y.encodeStateAsUpdate(room);
  room.getMap(DRAWING_APP_STATE_KEY).set("gridSize", 20);

  room.transact(() => restoreDrawingInto(room, backup));

  assert.equal(room.getMap(DRAWING_APP_STATE_KEY).has("gridSize"), false);
});

test("an empty backup erases the canvas", () => {
  const { room } = editedSinceBackup();

  const result = room.transact(() => restoreDrawingInto(room, Y.encodeStateAsUpdate(new Y.Doc())));

  assert.deepEqual(live(room), []);
  assert.deepEqual(result, { restored: 0, removed: 2 });
});

test("a backup stored with no state at all is an empty canvas", () => {
  const { room } = editedSinceBackup();

  const result = room.transact(() => restoreDrawingInto(room, new Uint8Array()));

  assert.deepEqual(live(room), []);
  assert.deepEqual(result, { restored: 0, removed: 2 });
});

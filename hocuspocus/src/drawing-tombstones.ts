// Removing the tombstones of shapes erased long ago.
//
// Erasing a shape does not remove its key. It writes the element back with
// `isDeleted: true` at a higher version, because a peer that still holds the
// shape would otherwise write it straight back into the room. That tombstone
// carries the whole element, including every point of a freehand stroke. So
// a drawing that has been worked on for a while carries every shape ever
// erased from it: in the room, in content_state, in every backup and
// transfer, and in the initial sync of every browser that opens it.
//
// Once a tombstone is old, the case it protects against is a tab that has
// held the shape, unsynced, for longer than TOMBSTONE_MAX_AGE_MS. Removing the
// key is safe for every canvas that saw the erase: it already shows the
// shape as gone. A canvas that missed it learns of the removal through the
// key-delete event and hides the shape (hideRemovedElements in the frontend's
// drawing-scene.ts). Only a canvas that changes the shape after the removal
// brings it back. That is somebody editing it, which is a real edit.
//
// Y.js keeps a removed key as a few bytes of deletion record. Garbage
// collection drops the value itself, which is where the size is.

import * as Y from "yjs";

import { DRAWING_ROOT_KEY } from "./drawing-projection.js";

/** A week, as proposed. Excalidraw's own collaboration server uses a day; a
 * week leaves room for a laptop shut over a long weekend with the page open. */
export const TOMBSTONE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

/**
 * Remove every tombstone last changed more than `maxAgeMs` before `now`.
 * Returns how many were removed.
 *
 * `updated` is the erasing client's clock. A tombstone without one, or dated
 * in the future, is kept: no age is known, and keeping one is never wrong.
 */
export function pruneDrawingTombstones(
  doc: Y.Doc,
  now: number = Date.now(),
  maxAgeMs: number = TOMBSTONE_MAX_AGE_MS,
): number {
  const elements = doc.getMap<Record<string, unknown>>(DRAWING_ROOT_KEY);
  const stale: string[] = [];
  for (const [id, el] of elements.entries()) {
    if (typeof el !== "object" || el === null || el.isDeleted !== true) continue;
    const updated = el.updated;
    if (typeof updated !== "number" || !Number.isFinite(updated)) continue;
    if (updated > now) continue;
    if (now - updated > maxAgeMs) stale.push(id);
  }
  if (stale.length === 0) return 0;
  doc.transact(() => {
    for (const id of stale) elements.delete(id);
  });
  return stale.length;
}

/**
 * A drawing's stored state with its old tombstones removed, or the state
 * unchanged when there are none.
 *
 * Applied when a room is loaded, before anyone is in it. The removal is
 * encoded as ordinary Y.js deletions, so a browser that reconnects holding
 * the tombstones merges them away rather than restoring them. It reaches
 * Mongo with the room's next save. It is not written back on its own,
 * because a load is not an edit. Until then it is simply repeated on the
 * next load.
 */
export function pruneStoredDrawing(
  state: Uint8Array,
  now: number = Date.now(),
): { state: Uint8Array; removed: number } {
  const doc = new Y.Doc();
  try {
    Y.applyUpdate(doc, state);
    const removed = pruneDrawingTombstones(doc, now);
    return removed === 0 ? { state, removed } : { state: Y.encodeStateAsUpdate(doc), removed };
  } finally {
    doc.destroy();
  }
}

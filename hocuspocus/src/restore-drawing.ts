// Restore a drawing page to a backup, inside its live room.
//
// The obvious implementation — write the backup's bytes over content_state in
// Mongo — does not work for any page somebody has open, and does not work
// reliably for one nobody has open either:
//
//   - A loaded room holds its own Y.Doc in memory and never re-reads Mongo
//     while it is loaded, so its next debounced store() writes the current
//     scene straight back over the restore.
//   - Evicting the room first does not help. Every browser that had the page
//     open still holds the newer state, and on reconnect Y.js merges it into
//     the restored bytes — a CRDT merge is a union, so every shape drawn since
//     the backup comes back and the restore is quietly undone.
//
// So a restore is performed the way any other edit is: as one transaction in
// the room, which every connected canvas takes and which persists through the
// normal store. That only works if every canvas *accepts* the change, and a
// canvas takes an element from the room only when its version is higher than
// the one it holds (see shouldKeepLocal in the frontend's drawing-scene.ts).
// The backup's elements are older by definition, so each one is written back
// at a version above both its backup copy and whatever is on the canvas now.

import * as Y from "yjs";

import { DRAWING_ROOT_KEY } from "./drawing-projection.js";
import { DRAWING_APP_STATE_KEY } from "./yjs-to-excalidraw.js";
import { applyStoredState } from "./stored-state.js";

type SceneElement = Record<string, unknown> & { version?: unknown; isDeleted?: unknown };

/** The appState fields a drawing shares. Mirrors SharedAppState in the
 * frontend's drawing-scene.ts; everything else in appState is per viewer. */
const SHARED_APP_STATE_FIELDS = ["viewBackgroundColor", "gridSize"] as const;

export interface RestoreDrawingResult {
  /** Elements put back to their backup copy, including ones that had been
   * erased since and are brought back. */
  restored: number;
  /** Live elements erased because the backup did not have them. */
  removed: number;
}

function versionOf(el: SceneElement | undefined): number {
  const v = el?.version;
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

/** A fresh nonce. Excalidraw breaks equal-version ties on it, and a restored
 * element must not tie with anything a canvas already holds. */
function randomNonce(): number {
  return Math.floor(Math.random() * 2 ** 31);
}

/**
 * Make `target`'s scene the scene held in `backupState`.
 *
 * Must be called inside a transaction on the target document, so that
 * collaborators see one change rather than the canvas emptying and refilling.
 *
 * Elements are tombstoned rather than removed for the same reason
 * apply-drawing's delete does it: a peer that still holds a removed key would
 * write it straight back, while a tombstone at a higher version wins.
 */
export function restoreDrawingInto(
  target: Y.Doc,
  backupState: Uint8Array,
): RestoreDrawingResult {
  const backup = new Y.Doc();
  try {
    applyStoredState(backup, backupState);

    const wanted = new Map<string, SceneElement>();
    for (const [id, el] of backup.getMap<SceneElement>(DRAWING_ROOT_KEY).entries()) {
      // A tombstone in the backup is a shape that was already gone when the
      // snapshot was taken, which is the same as the backup not having it.
      if (typeof el !== "object" || el === null || el.isDeleted === true) continue;
      wanted.set(id, el);
    }

    const elements = target.getMap<SceneElement>(DRAWING_ROOT_KEY);
    let restored = 0;
    let removed = 0;

    for (const [id, current] of elements.entries()) {
      if (wanted.has(id)) continue;
      if (typeof current !== "object" || current === null || current.isDeleted === true) continue;
      elements.set(id, {
        ...current,
        isDeleted: true,
        version: versionOf(current) + 1,
        versionNonce: randomNonce(),
        updated: Date.now(),
      });
      removed++;
    }

    for (const [id, el] of wanted) {
      const current = elements.get(id);
      elements.set(id, {
        ...el,
        isDeleted: false,
        version: Math.max(versionOf(el), versionOf(current)) + 1,
        versionNonce: randomNonce(),
        updated: Date.now(),
      });
      restored++;
    }

    const appState = target.getMap(DRAWING_APP_STATE_KEY);
    const backupAppState = backup.getMap(DRAWING_APP_STATE_KEY);
    for (const field of SHARED_APP_STATE_FIELDS) {
      if (backupAppState.has(field)) {
        appState.set(field, backupAppState.get(field));
      } else if (appState.has(field)) {
        // The backup predates anyone choosing this, so the page goes back to
        // the default rather than keeping a choice made after the snapshot.
        appState.delete(field);
      }
    }

    return { restored, removed };
  } finally {
    backup.destroy();
  }
}

import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { applyMode, elementsOf } from "../apply-drawing.js";
import { normalizeElements, type NormalizedElement } from "../drawing-elements.js";

const DAY = 24 * 60 * 60 * 1000;

function sceneWith(...els: Partial<NormalizedElement>[]): Y.Map<NormalizedElement> {
  const doc = new Y.Doc();
  const map = elementsOf(doc);
  for (const el of normalizeElements(els)) map.set(el.id, el);
  return map;
}

function rect(id: string, extra: Partial<NormalizedElement> = {}): Partial<NormalizedElement> {
  return { id, type: "rectangle", x: 0, y: 0, width: 100, height: 50, ...extra };
}

test("add over an existing id raises the version above the room's", () => {
  const map = sceneWith(rect("a", { version: 7, versionNonce: 3 }));
  // What an agent sends back after a read: the version it saw.
  applyMode(map, "add", normalizeElements([rect("a", { x: 40, version: 7, versionNonce: 3 })]), [], []);
  const a = map.get("a")!;
  assert.equal(a.x, 40);
  assert.equal(a.version, 8);
  assert.notEqual(a.versionNonce, 3);
});

test("add over an existing id without a version still beats the room's copy", () => {
  const map = sceneWith(rect("a", { version: 5 }));
  applyMode(map, "add", normalizeElements([rect("a", { x: 40 })]), [], []);
  assert.equal(map.get("a")!.version, 6);
});

test("replace writes a re-sent id above its own tombstone", () => {
  const map = sceneWith(rect("a", { version: 4 }), rect("b", { version: 2 }));
  applyMode(map, "replace", normalizeElements([rect("a", { x: 10, version: 4 })]), [], []);
  const a = map.get("a")!;
  assert.equal(a.isDeleted, false);
  // Tombstoned at 5, then written above it.
  assert.equal(a.version, 6);
  assert.equal(map.get("b")!.isDeleted, true);
  assert.equal(map.get("b")!.version, 3);
});

test("new ids keep the version they arrived with", () => {
  const map = sceneWith();
  applyMode(map, "add", normalizeElements([rect("n", { version: 1 })]), [], []);
  assert.equal(map.get("n")!.version, 1);
});

test("delete stamps the tombstone with the time of deletion", () => {
  const old = Date.now() - 30 * DAY;
  const map = sceneWith(rect("a", { updated: old }));
  const before = Date.now();
  applyMode(map, "delete", [], [], ["a"]);
  const a = map.get("a")!;
  assert.equal(a.isDeleted, true);
  assert.ok(Number(a.updated) >= before, "tombstone age must start at deletion");
});

test("replace stamps every tombstone with the time of deletion", () => {
  const old = Date.now() - 30 * DAY;
  const map = sceneWith(rect("a", { updated: old }));
  const before = Date.now();
  applyMode(map, "replace", normalizeElements([rect("b")]), [], []);
  assert.ok(Number(map.get("a")!.updated) >= before);
});

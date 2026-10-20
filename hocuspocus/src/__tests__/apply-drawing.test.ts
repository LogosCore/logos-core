import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";

import { applyMode, elementsOf, stackedGeometry } from "../apply-drawing.js";
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

/** Two labelled boxes joined by an arrow, written the way an agent writes them. */
function connected(): Y.Map<NormalizedElement> {
  const map = sceneWith();
  applyMode(
    map,
    "add",
    normalizeElements([
      rect("a", { label: "Web" } as Partial<NormalizedElement>),
      rect("b", { x: 300, label: "DB" } as Partial<NormalizedElement>),
      { id: "arr", type: "arrow", startBinding: "a", endBinding: "b" },
    ]),
    [],
    [],
  );
  return map;
}

test("deleting a labelled shape deletes its label", () => {
  const map = connected();
  assert.equal(map.get("a-label")?.isDeleted, false, "fixture: the label is live");
  applyMode(map, "delete", [], [], ["a"]);
  assert.equal(map.get("a-label")!.isDeleted, true);
  assert.equal(map.get("b-label")!.isDeleted, false, "only the deleted shape's label goes");
});

test("deleting a shape detaches the arrows bound to it", () => {
  const map = connected();
  const before = map.get("arr")!.version;
  applyMode(map, "delete", [], [], ["a"]);
  const arrow = map.get("arr")!;
  assert.equal(arrow.isDeleted, false);
  assert.equal(arrow.startBinding, null);
  assert.equal((arrow.endBinding as { elementId: string }).elementId, "b");
  assert.ok(arrow.version > before, "the detach must bump the version or no canvas takes it");
});

test("deleting an arrow drops it from the shapes it was bound to", () => {
  const map = connected();
  const ids = (el: NormalizedElement) => ((el.boundElements as { id: string }[] | null) ?? []).map((b) => b.id);
  assert.ok(ids(map.get("b")!).includes("arr"), "fixture: the shape lists the arrow");
  applyMode(map, "delete", [], [], ["arr"]);
  assert.ok(!ids(map.get("a")!).includes("arr"));
  assert.ok(!ids(map.get("b")!).includes("arr"));
  assert.ok(ids(map.get("b")!).includes("b-label"), "the label stays bound");
});

test("a label found only by its containerId is deleted too", () => {
  const map = sceneWith(rect("box"), { id: "t", type: "text", text: "hi", containerId: "box" });
  applyMode(map, "delete", [], [], ["box"]);
  assert.equal(map.get("t")!.isDeleted, true);
});

/** Three boxes in a row, and an arrow from a to b. */
function rebindable(): Y.Map<NormalizedElement> {
  const map = sceneWith();
  applyMode(
    map,
    "add",
    normalizeElements([
      rect("a"),
      rect("b", { x: 300 }),
      rect("c", { x: 0, y: 400 }),
      { id: "arr", type: "arrow", startBinding: "a", endBinding: "b" },
    ]),
    [],
    [],
  );
  return map;
}

const boundIDs = (el: NormalizedElement | undefined) =>
  ((el?.boundElements as { id: string }[] | null) ?? []).map((b) => b.id);

test("rebinding an arrow by update wires the new shape, even from a patch without type", () => {
  const map = rebindable();
  // What an agent sends: just the field it changes.
  applyMode(map, "update", [], [{ id: "arr", endBinding: { elementId: "c", focus: 0, gap: 4 } }], []);
  assert.equal((map.get("arr")!.endBinding as { elementId: string }).elementId, "c");
  assert.ok(boundIDs(map.get("c")).includes("arr"), "the new target lists the arrow");
});

test("rebinding an arrow by update unwires the old shape", () => {
  const map = rebindable();
  assert.ok(boundIDs(map.get("b")).includes("arr"), "fixture: b lists the arrow");
  applyMode(map, "update", [], [{ id: "arr", endBinding: { elementId: "c", focus: 0, gap: 4 } }], []);
  assert.ok(!boundIDs(map.get("b")).includes("arr"));
  assert.ok(boundIDs(map.get("a")).includes("arr"), "the unchanged end stays wired");
});

test("rebinding an arrow by update re-routes its stroke to the new shape", () => {
  const map = rebindable();
  const before = map.get("arr")!.points as number[][];
  applyMode(map, "update", [], [{ id: "arr", endBinding: { elementId: "c", focus: 0, gap: 4 } }], []);
  const after = map.get("arr")!.points as number[][];
  assert.notDeepEqual(after, before);
  // c is below a, so the stroke now heads down rather than across.
  const [dx, dy] = after[after.length - 1];
  assert.ok(dy > Math.abs(dx), `stroke ends at ${dx},${dy}; expected it to point down at c`);
});

test("an update that sends its own points keeps them when rebinding", () => {
  const map = rebindable();
  const points = [[0, 0], [10, 10]];
  applyMode(map, "update", [], [{ id: "arr", endBinding: { elementId: "c", focus: 0, gap: 4 }, points }], []);
  assert.deepEqual(map.get("arr")!.points, points);
});

test("an update to a deleted shape is skipped, not counted", () => {
  const map = sceneWith(rect("a"));
  applyMode(map, "delete", [], [], ["a"]);
  const tombstone = map.get("a")!;
  const applied = applyMode(map, "update", [], [{ id: "a", x: 99 }], []);
  assert.equal(applied, 0);
  assert.deepEqual(map.get("a"), tombstone);
});

test("deleting an already-deleted shape is not counted again", () => {
  const map = sceneWith(rect("a"));
  applyMode(map, "delete", [], [], ["a"]);
  assert.equal(applyMode(map, "delete", [], [], ["a"]), 0);
});

test("stacking is reported for shapes this write stacked", () => {
  const map = sceneWith(rect("a"), rect("b", { x: 300 }));
  applyMode(map, "add", normalizeElements([rect("c"), rect("d")]), [], []);
  // c and d land on a: three in one pile.
  assert.match(stackedGeometry(map.values(), new Set(["c", "d"])) ?? "", /^3 of these shapes/);
});

test("a shape written on top of one already there is reported", () => {
  const map = sceneWith(rect("a"));
  applyMode(map, "add", normalizeElements([rect("n")]), [], []);
  assert.match(stackedGeometry(map.values(), new Set(["n"])) ?? "", /^2 of these shapes/);
});

test("a pile already on the canvas is not blamed on an unrelated write", () => {
  const map = sceneWith(rect("a"), rect("b"));
  applyMode(map, "add", normalizeElements([rect("n", { x: 500 })]), [], []);
  assert.equal(stackedGeometry(map.values(), new Set(["n"])), null);
});

test("deleted shapes and a delete write stack nothing", () => {
  const map = sceneWith(rect("a"), rect("b"));
  applyMode(map, "delete", [], [], ["b"]);
  assert.equal(stackedGeometry(map.values(), new Set(["a"])), null);
  assert.equal(stackedGeometry(map.values(), new Set()), null);
});

// --- Arrow labels --------------------------------------------------------

function labelOf(map: Y.Map<NormalizedElement>, arrowID: string): NormalizedElement {
  const bound = map.get(arrowID)!.boundElements as { id: string; type: string }[];
  const id = bound.find((b) => b.type === "text")!.id;
  return map.get(id)!;
}

test("an arrow's label keeps the arrow's geometry and sits on its middle", () => {
  const map = sceneWith();
  applyMode(map, "add", normalizeElements([
    { id: "m", type: "arrow", x: 100, y: 200, points: [[0, 0], [300, 0]], label: "GET /distribution-strategy/{id}/distribute" },
  ]), [], []);
  const arrow = map.get("m")!;
  // Not sized like a box: a flat arrow stays flat.
  assert.equal(arrow.height, 0);
  assert.equal(arrow.width, 300);
  const label = labelOf(map, "m");
  assert.equal(label.containerId, "m");
  // Excalidraw's own wrap width: max(0.7 * 300, 16 * 11) = 210.
  assert.ok(Number(label.width) <= 210, `width ${label.width}`);
  assert.ok(String(label.text).includes("\n"), "a long label wraps to the arrow");
  assert.equal(label.originalText, "GET /distribution-strategy/{id}/distribute");
  const cx = Number(label.x) + Number(label.width) / 2;
  const cy = Number(label.y) + Number(label.height) / 2;
  assert.deepEqual([Math.round(cx), Math.round(cy)], [250, 200]);
});

test("a bound arrow's label is fitted once the server has placed the arrow", () => {
  const map = sceneWith(rect("a", { x: 0, y: 0 }), rect("b", { x: 600, y: 0 }));
  applyMode(map, "add", normalizeElements([
    { id: "e", type: "arrow", startBinding: "a", endBinding: "b", label: "pass-the-hash" },
  ]), [], []);
  const arrow = map.get("e")!;
  const label = labelOf(map, "e");
  const mid = Number(arrow.x) + (arrow.points as number[][])[1][0] / 2;
  assert.equal(Math.round(Number(label.x) + Number(label.width) / 2), Math.round(mid));
  assert.equal(Math.round(Number(label.y) + Number(label.height) / 2), 25);
});

test("moving a labelled arrow by update moves its label with it", () => {
  const map = sceneWith();
  applyMode(map, "add", normalizeElements([
    { id: "m", type: "arrow", x: 0, y: 0, points: [[0, 0], [200, 0]], label: "ping" },
  ]), [], []);
  const before = labelOf(map, "m");
  applyMode(map, "update", [], [{ id: "m", y: 120 }], []);
  const after = labelOf(map, "m");
  assert.equal(Number(after.y) - Number(before.y), 120);
  assert.ok(after.version > before.version);
});

test("label on an update rewraps an arrow's words to the arrow", () => {
  const map = sceneWith();
  applyMode(map, "add", normalizeElements([
    { id: "m", type: "arrow", x: 0, y: 0, points: [[0, 0], [260, 0]], label: "ping" },
  ]), [], []);
  applyMode(map, "update", [], [{ id: "m", label: "POST /messages/draft subject=requestId text=/path + meta lines" }], []);
  const label = labelOf(map, "m");
  assert.equal(label.originalText, "POST /messages/draft subject=requestId text=/path + meta lines");
  assert.ok(String(label.text).includes("\n"));
  assert.ok(Number(label.width) <= Math.max(0.7 * 260, 176));
});

test("label on an update gives a shape without one a label, instead of nothing", () => {
  const map = sceneWith(rect("a"), { id: "m", type: "arrow", x: 0, y: 100, points: [[0, 0], [200, 0]] });
  assert.equal(applyMode(map, "update", [], [{ id: "a", label: "DC01" }, { id: "m", label: "pong" }], []), 2);
  assert.equal(labelOf(map, "a").text, "DC01");
  assert.equal(labelOf(map, "a").containerId, "a");
  assert.equal(labelOf(map, "m").text, "pong");
  assert.equal(Math.round(Number(labelOf(map, "m").y) + Number(labelOf(map, "m").height) / 2), 100);
});

import { test } from "node:test";
import assert from "node:assert/strict";

import { normalizeElements } from "../drawing-elements.js";
import { textCollisions, textCollisionWarning } from "../text-overlap.js";

const text = (id: string, x: number, y: number, words = "two\nlines") =>
  ({ id, type: "text", x, y, text: words, fontSize: 15 });

test("text running down across an arrow is a collision; text above it is not", () => {
  const scene = normalizeElements([
    { id: "a", type: "arrow", x: 0, y: 100, points: [[0, 0], [300, 0]] },
    text("over", 50, 80),
    text("above", 50, 40),
  ]);
  assert.deepEqual(textCollisions(scene), [{ text: "over", other: "a", kind: "arrow" }]);
});

test("a caption inside a frame is fine; one across its border is not", () => {
  const scene = normalizeElements([
    { id: "f", type: "rectangle", x: 0, y: 100, width: 400, height: 200 },
    text("inside", 10, 110, "loop [every 5s]"),
    text("across", 300, 80),
  ]);
  assert.deepEqual(textCollisions(scene), [{ text: "across", other: "f", kind: "border" }]);
});

test("overlapping free texts collide; a bound label is not checked", () => {
  const scene = normalizeElements([
    text("one", 0, 0),
    text("two", 10, 10),
    { id: "box", type: "rectangle", x: 0, y: 0, width: 60, height: 40, label: "x" },
  ]);
  const kinds = textCollisions(scene).map((c) => `${c.text}/${c.other}/${c.kind}`);
  assert.ok(kinds.includes("one/two/text"), kinds.join(" "));
  assert.ok(!kinds.some((k) => k.includes("box-label")), "the box's own label is laid out by Excalidraw");
});

test("only collisions involving what a write touched are reported to it", () => {
  const scene = normalizeElements([
    { id: "a", type: "arrow", x: 0, y: 100, points: [[0, 0], [300, 0]] },
    text("old", 50, 80),
    text("new", 50, 300),
  ]);
  assert.deepEqual(textCollisions(scene, new Set(["new"])), []);
  assert.equal(textCollisions(scene, new Set(["a"])).length, 1);
});

test("the warning names each collision and what to do", () => {
  const w = textCollisionWarning([{ text: "t1", other: "a1", kind: "arrow" }])!;
  assert.match(w, /text t1 runs across arrow a1/);
  assert.match(w, /`label`/);
  assert.equal(textCollisionWarning([]), null);
});

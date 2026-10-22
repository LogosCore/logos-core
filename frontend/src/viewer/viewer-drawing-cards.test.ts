import { expect, test } from "vitest"
import { cardAt } from "./viewer-drawing-cards"

const part = (id: string, x: number, ref?: { kind: string; id: string }) => ({
  id,
  x,
  y: 0,
  width: 100,
  height: 50,
  customData: ref ? { logos: { shape: "card:server", part: "frame", ref } } : undefined,
})

test("the record card under a point, topmost first", () => {
  const scene = [
    part("a", 0, { kind: "host", id: "h1" }),
    part("b", 50, { kind: "task", id: "t1" }),
    part("plain", 0),
  ]
  expect(cardAt(scene, { x: 10, y: 10 })).toBe("host:h1")
  expect(cardAt(scene, { x: 60, y: 10 })).toBe("task:t1")
  expect(cardAt(scene, { x: 200, y: 10 })).toBeNull()
  expect(cardAt([{ ...scene[0], isDeleted: true }], { x: 10, y: 10 })).toBeNull()
})

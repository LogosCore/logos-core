import { describe, expect, test } from "vitest"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"

import { boundsOf, centeredAt, viewportCenter, withFreshIds } from "./drawing-insert"

const el = (fields: Record<string, unknown>) =>
  ({ x: 0, y: 0, width: 10, height: 10, groupIds: [], frameId: null, index: "a0", version: 1, versionNonce: 1, ...fields }) as unknown as ExcalidrawElement

/** A labelled box and an arrow bound to it, grouped: every kind of
 * reference an inserted shape carries. */
function card() {
  return [
    el({ id: "box", type: "rectangle", groupIds: ["g"], boundElements: [{ id: "label", type: "text" }, { id: "arrow", type: "arrow" }] }),
    el({ id: "label", type: "text", groupIds: ["g"], containerId: "box" }),
    el({ id: "arrow", type: "arrow", groupIds: ["g"], points: [[0, 0], [50, 0]], startBinding: { elementId: "box", focus: 0, gap: 1 }, endBinding: { elementId: "elsewhere", focus: 0, gap: 1 } }),
  ]
}

function counter() {
  let n = 0
  return () => `id${++n}`
}

describe("withFreshIds", () => {
  test("every element and group gets a new id", () => {
    const copy = withFreshIds(card(), counter())
    expect(copy.map((e) => e.id)).toEqual(["id1", "id2", "id3"])
    expect(new Set(copy.map((e) => e.groupIds.join()))).toEqual(new Set(["id4"]))
  })

  // Two inserts of one card must neither overwrite each other in the room nor
  // select and move as one.
  test("two copies share no element or group id", () => {
    const next = counter()
    const a = withFreshIds(card(), next)
    const b = withFreshIds(card(), next)
    const ids = [...a, ...b].map((e) => e.id)
    expect(new Set(ids).size).toBe(6)
    expect(a[0].groupIds[0]).not.toBe(b[0].groupIds[0])
  })

  test("labels, containers and arrow ends follow the new ids", () => {
    type Row = {
      id: string
      containerId?: string
      boundElements?: { id: string }[]
      startBinding?: { elementId: string }
    }
    const [box, label, arrow] = withFreshIds(card(), counter()) as unknown as Row[]
    expect(label.containerId).toBe(box.id)
    expect(box.boundElements?.map((b) => b.id)).toEqual([label.id, arrow.id])
    expect(arrow.startBinding?.elementId).toBe(box.id)
  })

  test("a reference to something not copied is dropped, not left dangling", () => {
    const arrow = withFreshIds(card(), counter())[2] as unknown as { endBinding: unknown }
    expect(arrow.endBinding).toBeNull()
  })

  test("the canvas is left to place the copy in paint order", () => {
    for (const e of withFreshIds(card(), counter())) expect(e.index).toBeNull()
  })

  test("the source is not modified", () => {
    const source = card()
    withFreshIds(source, counter())
    expect(source[0].id).toBe("box")
  })
})

describe("placing a copy", () => {
  test("a line's box comes from its points, which can reach behind its origin", () => {
    expect(boundsOf([el({ type: "line", x: 100, y: 100, points: [[0, 0], [-20, 30]] })])).toEqual({
      minX: 80,
      minY: 100,
      maxX: 100,
      maxY: 130,
    })
  })

  test("the centre of the view, in scene coordinates", () => {
    expect(viewportCenter({ scrollX: -100, scrollY: 50, width: 800, height: 600, zoom: { value: 2 } })).toEqual({
      x: 300,
      y: 100,
    })
  })

  test("a copy is centred on the point it is placed at", () => {
    const placed = centeredAt([el({ x: 0, y: 0, width: 100, height: 40 })], { x: 500, y: 300 })
    expect(boundsOf(placed)).toEqual({ minX: 450, minY: 280, maxX: 550, maxY: 320 })
  })
})

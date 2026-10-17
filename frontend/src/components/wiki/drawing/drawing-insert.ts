// Putting a shape from the Shapes panel onto the canvas.
//
// Excalidraw has no public "insert these elements" call; its own library
// panel uses an internal one. So a click in our panel inserts through
// updateScene. That needs two things Excalidraw would otherwise do itself:
//
//   - every copy gets fresh ids. Two inserts of the same card must not share
//     element ids, which would make the second overwrite the first in the
//     room. Nor may they share group ids, which would make them select and
//     move as one. Bindings (a label's container, an arrow's ends, a frame)
//     follow the new ids.
//   - the copy lands where the operator is looking, not at the shape's own
//     origin, which may be far off screen.
//
// Pure, so it is tested without a browser. Dragging from the panel does not
// come through here: it hands the canvas a library item, and Excalidraw's own
// drop handling places and re-ids it.

import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"

type Mutable<T> = { -readonly [K in keyof T]: T[K] }
type AnyElement = Mutable<ExcalidrawElement> & {
  containerId?: string | null
  boundElements?: readonly { id: string; type: string }[] | null
  startBinding?: { elementId: string } | null
  endBinding?: { elementId: string } | null
  points?: readonly (readonly [number, number])[]
}

export function randomId(): string {
  const bytes = new Uint8Array(12)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(36).padStart(2, "0")).join("").slice(0, 20)
}

/**
 * A copy of `elements` with new element and group ids, and every reference
 * between them rewritten. A reference to something outside the set is
 * dropped rather than left pointing at another copy.
 */
export function withFreshIds(
  elements: readonly ExcalidrawElement[],
  newId: () => string = randomId,
): ExcalidrawElement[] {
  const ids = new Map(elements.map((el) => [el.id, newId()]))
  const groups = new Map<string, string>()
  const group = (id: string) => {
    if (!groups.has(id)) groups.set(id, newId())
    return groups.get(id) as string
  }
  const ref = (id: string | null | undefined) => (id ? (ids.get(id) ?? null) : null)

  return elements.map((source) => {
    const el = { ...source } as AnyElement
    el.id = ids.get(source.id) as string
    el.groupIds = source.groupIds.map(group)
    el.frameId = ref(source.frameId)
    // Excalidraw assigns a fresh fractional index to an element without one,
    // after everything already on the canvas.
    ;(el as { index: string | null }).index = null
    el.versionNonce = Math.floor(Math.random() * 2 ** 31)
    if ("containerId" in el) el.containerId = ref(el.containerId)
    if (el.boundElements) {
      el.boundElements = el.boundElements
        .filter((b) => ids.has(b.id))
        .map((b) => ({ ...b, id: ids.get(b.id) as string }))
    }
    for (const end of ["startBinding", "endBinding"] as const) {
      const binding = el[end]
      if (binding) {
        const target = ref(binding.elementId)
        el[end] = target ? { ...binding, elementId: target } : null
      }
    }
    return el as ExcalidrawElement
  })
}

interface Bounds {
  minX: number
  minY: number
  maxX: number
  maxY: number
}

/** The box around a set of elements. A line's box comes from its points,
 * which can reach left of or above its origin. */
export function boundsOf(elements: readonly ExcalidrawElement[]): Bounds {
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const el of elements as readonly AnyElement[]) {
    if (el.points && el.points.length > 0) {
      for (const [px, py] of el.points) {
        minX = Math.min(minX, el.x + px)
        minY = Math.min(minY, el.y + py)
        maxX = Math.max(maxX, el.x + px)
        maxY = Math.max(maxY, el.y + py)
      }
    } else {
      minX = Math.min(minX, el.x)
      minY = Math.min(minY, el.y)
      maxX = Math.max(maxX, el.x + el.width)
      maxY = Math.max(maxY, el.y + el.height)
    }
  }
  return { minX, minY, maxX, maxY }
}

/** The scene point at the centre of the canvas, from the slice of
 * Excalidraw's appState that says where the viewport is. */
export function viewportCenter(view: {
  scrollX: number
  scrollY: number
  width: number
  height: number
  zoom: { value: number }
}): { x: number; y: number } {
  return {
    x: view.width / 2 / view.zoom.value - view.scrollX,
    y: view.height / 2 / view.zoom.value - view.scrollY,
  }
}

/** `elements` moved so their box is centred on `center`. */
export function centeredAt(
  elements: readonly ExcalidrawElement[],
  center: { x: number; y: number },
): ExcalidrawElement[] {
  const b = boundsOf(elements)
  const dx = center.x - (b.minX + b.maxX) / 2
  const dy = center.y - (b.minY + b.maxY) / 2
  return elements.map((el) => ({ ...el, x: el.x + dx, y: el.y + dy }) as ExcalidrawElement)
}

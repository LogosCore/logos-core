// Which record card a point on an exported drawing falls on.
//
// The canvas has the same test (drawing-record-cards.ts, recordAt). It is not
// imported from there because that module reads tags through the shape
// catalogue, and the catalogue carries the drawn geometry of every icon,
// which this bundle would then ship to read one field.

type SceneElement = {
  isDeleted?: boolean
  x: number
  y: number
  width: number
  height: number
  points?: [number, number][]
  customData?: { logos?: { ref?: { kind?: unknown; id?: unknown } } }
}

/** "kind:id" of the record card under a scene point, topmost first. */
export function cardAt(elements: readonly unknown[], point: { x: number; y: number }): string | null {
  for (let i = elements.length - 1; i >= 0; i--) {
    const el = elements[i] as SceneElement
    if (!el || el.isDeleted) continue
    const ref = el.customData?.logos?.ref
    if (!ref || typeof ref.kind !== "string" || typeof ref.id !== "string") continue
    let { x, y, width, height } = el
    if (el.points?.length) {
      const xs = el.points.map((p) => el.x + p[0])
      const ys = el.points.map((p) => el.y + p[1])
      x = Math.min(...xs)
      y = Math.min(...ys)
      width = Math.max(...xs) - x
      height = Math.max(...ys) - y
    }
    if (point.x >= x && point.x <= x + width && point.y >= y && point.y <= y + height) return `${ref.kind}:${ref.id}`
  }
  return null
}

// The arithmetic of the drawing viewer's pan and zoom, kept apart from the
// DOM so it can be tested. A view is a scale and the screen position of the
// content's top-left corner: a content point p shows at p * scale + (x, y).

export interface View {
  scale: number
  x: number
  y: number
}

export const MIN_SCALE = 0.05
export const MAX_SCALE = 8

export function clampScale(scale: number): number {
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, scale))
}

/**
 * The view that shows the whole content centred in the viewport, never
 * enlarged past its own size — a small diagram opens at 100%, a large one
 * shrinks to fit.
 */
export function fitView(content: { width: number; height: number }, viewport: { width: number; height: number }, padding = 16): View {
  const w = Math.max(1, content.width)
  const h = Math.max(1, content.height)
  const scale = clampScale(Math.min(1, (viewport.width - 2 * padding) / w, (viewport.height - 2 * padding) / h))
  return {
    scale,
    x: (viewport.width - w * scale) / 2,
    y: (viewport.height - h * scale) / 2,
  }
}

/** Zooms by `factor` keeping the content point under (px, py) where it is. */
export function zoomAt(view: View, factor: number, px: number, py: number): View {
  const scale = clampScale(view.scale * factor)
  const k = scale / view.scale
  return { scale, x: px - (px - view.x) * k, y: py - (py - view.y) * k }
}

/** The view at an exact scale, zoomed about (px, py). */
export function zoomTo(view: View, scale: number, px: number, py: number): View {
  return zoomAt(view, clampScale(scale) / view.scale, px, py)
}

export function panBy(view: View, dx: number, dy: number): View {
  return { ...view, x: view.x + dx, y: view.y + dy }
}

/** Zoom factor for one wheel event's delta: smooth for trackpads, one
 * notch is about 10% for a mouse. */
export function wheelZoomFactor(deltaY: number, deltaMode: number): number {
  const pixels = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 400 : deltaY
  return Math.exp(-pixels * 0.0015)
}

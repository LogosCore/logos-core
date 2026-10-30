import { useEffect, useRef } from "react"
import { ROWS, burnFrame, newFire, type Fire } from "./session-fire-model"

// The pixel fire behind an On Fire row: the heats session-fire-model.ts
// burns, painted FPS times a second on a canvas ROWS pixels tall whatever
// the row's height, as wide as square pixels make it, scaled up crisp (as
// in the "CSS Pixel Fire Animation" pen, https://codepen.io/anon/pen/REKKEM).

const FPS = 20

type Rgba = readonly [number, number, number, number]

// The colors of heats 1 to LEVELS - 1 (0 is none), in either theme; the
// coolest are see-through, so a flame fades at its edges. On dark, Doom's
// own ramp, sampled from its 37 colors: dark red, orange, yellow, white.
const DARK: readonly Rgba[] = [
  [0x47, 0x0f, 0x07, 100],
  [0x77, 0x1f, 0x07, 140],
  [0x9f, 0x2f, 0x07, 180],
  [0xc7, 0x47, 0x07, 220],
  [0xdf, 0x57, 0x07, 255],
  [0xd7, 0x67, 0x0f, 255],
  [0xcf, 0x77, 0x0f, 255],
  [0xc7, 0x87, 0x17, 255],
  [0xbf, 0x9f, 0x1f, 255],
  [0xbf, 0xa7, 0x27, 255],
  [0xb7, 0xaf, 0x2f, 255],
  [0xcf, 0xcf, 0x6f, 255],
  [0xff, 0xff, 0xff, 255],
]

// On white Doom's dark reds turn to mud: red at the edges instead, then
// orange, amber and yellow at the heart.
const LIGHT = ramp(DARK.length, [
  [0, [239, 68, 68, 70]],
  [0.2, [234, 88, 12, 200]],
  [0.45, [249, 115, 22, 255]],
  [0.75, [245, 158, 11, 255]],
  [1, [250, 204, 21, 255]],
])

/** count colors blended along stops, from the first stop to the last. */
function ramp(count: number, stops: readonly (readonly [number, Rgba])[]): Rgba[] {
  return Array.from({ length: count }, (_, i) => {
    const t = i / (count - 1)
    const k = Math.max(0, stops.findIndex(([at]) => at >= t) - 1)
    const [t0, c0] = stops[k]
    const [t1, c1] = stops[k + 1]
    const u = t1 > t0 ? (t - t0) / (t1 - t0) : 0
    return c0.map((v, j) => Math.round(v + (c1[j] - v) * u)) as unknown as Rgba
  })
}

interface Burning {
  fire: Fire
  image: ImageData
}

const state = new WeakMap<HTMLCanvasElement, Burning>()

function draw(canvas: HTMLCanvasElement) {
  const ctx = canvas.getContext("2d")
  const width = canvas.width
  if (!ctx || width === 0) return
  let burning = state.get(canvas)
  if (!burning || burning.fire.width !== width) {
    // Lit already burning, here and when the row's width changes.
    burning = { fire: newFire(width), image: ctx.createImageData(width, ROWS) }
    state.set(canvas, burning)
  } else {
    burnFrame(burning.fire)
  }
  const colors = canvas.closest(".dark") ? DARK : LIGHT
  const { heat } = burning.fire
  const px = burning.image.data
  for (let p = 0; p < width * ROWS; p++) {
    const h = heat[p]
    const o = p * 4
    if (h === 0) {
      px[o + 3] = 0
      continue
    }
    const c = colors[h - 1]
    px[o] = c[0]
    px[o + 1] = c[1]
    px[o + 2] = c[2]
    px[o + 3] = c[3]
  }
  ctx.putImageData(burning.image, 0, 0)
}

// One timer for every fire on screen, not one per row.
const fires = new Set<HTMLCanvasElement>()
let timer: ReturnType<typeof setInterval> | null = null

function burn(canvas: HTMLCanvasElement) {
  fires.add(canvas)
  draw(canvas)
  // Under reduced motion a fire is drawn once and holds still.
  if (timer === null && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    timer = setInterval(() => fires.forEach(draw), 1000 / FPS)
  }
  return () => {
    fires.delete(canvas)
    if (fires.size === 0 && timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }
}

/** The pixel fire behind an On Fire row (session-mark-styles.css: .mark-fx-fire). */
export function SessionFire() {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    const canvas = ref.current
    if (!canvas) return
    // As many columns as square pixels need across the row.
    const fit = () => {
      const { clientWidth, clientHeight } = canvas
      if (clientHeight === 0) return
      const width = Math.ceil(clientWidth / (clientHeight / ROWS))
      if (canvas.width !== width) {
        canvas.width = width
        draw(canvas)
      }
    }
    fit()
    const observer = new ResizeObserver(fit)
    observer.observe(canvas)
    const stop = burn(canvas)
    return () => {
      observer.disconnect()
      stop()
    }
  }, [])
  return <canvas ref={ref} aria-hidden className="mark-fx-fire" height={ROWS} width={0} />
}

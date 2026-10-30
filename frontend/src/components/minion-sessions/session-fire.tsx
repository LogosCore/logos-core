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

// One fire for every On Fire row on screen, not one each: a simulation
// costs about half a millisecond a frame at a row's width, so a screenful of
// burning rows took a third of a core. The fire is SPREAD columns wider than
// the widest row, and each row shows its own window of it, from an offset of
// its own, so the rows do not burn in step.
const SPREAD = 512

interface Shared {
  fire: Fire
  image: ImageData
  /** The fire painted, once a frame, for the rows to copy their windows of. */
  screen: HTMLCanvasElement
  /** Whether it was last painted in the dark palette; null: not yet. */
  dark: boolean | null
}

let shared: Shared | null = null
const fires = new Set<HTMLCanvasElement>()
const offsets = new WeakMap<HTMLCanvasElement, number>()
let timer: ReturnType<typeof setInterval> | null = null

/** The shared fire, wide enough for every row on screen; null without one. */
function sharedFire(): Shared | null {
  let widest = 0
  for (const canvas of fires) widest = Math.max(widest, canvas.width)
  if (widest === 0) return null
  if (!shared || shared.fire.width < widest + SPREAD) {
    const width = widest + SPREAD
    const screen = document.createElement("canvas")
    screen.width = width
    screen.height = ROWS
    // Lit already burning, here and when a row grows past it.
    shared = { fire: newFire(width), image: new ImageData(width, ROWS), screen, dark: null }
  }
  return shared
}

/** Paints the fire's heats in the rows' theme, onto its screen. */
function paint(s: Shared) {
  const dark = !!fires.values().next().value?.closest(".dark")
  const colors = dark ? DARK : LIGHT
  const { heat, width } = s.fire
  const px = s.image.data
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
  s.screen.getContext("2d")?.putImageData(s.image, 0, 0)
  s.dark = dark
}

/** A row's window of the fire. */
function show(canvas: HTMLCanvasElement, s: Shared) {
  const ctx = canvas.getContext("2d")
  if (!ctx || canvas.width === 0) return
  ctx.clearRect(0, 0, canvas.width, ROWS)
  ctx.drawImage(s.screen, offsets.get(canvas) ?? 0, 0, canvas.width, ROWS, 0, 0, canvas.width, ROWS)
}

/** A frame on: the fire burns, is painted, and every row shows its window. */
function tick() {
  const s = sharedFire()
  if (!s) return
  burnFrame(s.fire)
  paint(s)
  fires.forEach((canvas) => show(canvas, s))
}

/** Shows the fire as it stands, with no frame on: a row came or resized. */
function refresh() {
  const s = sharedFire()
  if (!s) return
  if (s.dark === null) paint(s)
  fires.forEach((canvas) => show(canvas, s))
}

function burn(canvas: HTMLCanvasElement) {
  offsets.set(canvas, Math.floor(Math.random() * SPREAD))
  fires.add(canvas)
  refresh()
  // Under reduced motion the fire is shown as it stands and holds still.
  if (timer === null && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    timer = setInterval(tick, 1000 / FPS)
  }
  return () => {
    fires.delete(canvas)
    if (fires.size > 0) return
    if (timer !== null) clearInterval(timer)
    timer = null
    shared = null
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
        refresh()
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

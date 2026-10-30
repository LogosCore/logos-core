import { useEffect, useRef } from "react"

// The pixel fire behind an On Fire row. Adapted from the "CSS Pixel Fire
// Animation" pen (https://codepen.io/anon/pen/REKKEM, MIT as public CodePen
// pens are): a tiny canvas scaled up with crisp pixels, redrawn a few times a
// second with each column of the flame at a random height between a floor
// and a ceiling, in three layers (red, orange, a pale core). One flame
// becomes a wall of them, side by side along the row; the canvas is ROWS
// pixels tall whatever the row's height, as wide as square pixels make it.

const FPS = 7
const ROWS = 20

// One flame, 8 columns wide (from the pen): where each column starts and the
// range its top lands in.
const BASE = [2, 1, 0, 0, 0, 0, 1, 2]
const MAX = [7, 9, 11, 13, 13, 11, 9, 7]
const MIN = [4, 7, 8, 10, 10, 8, 7, 4]
const WIDTH = BASE.length

const LAYERS: readonly { color: string; from: number; to: number; drop: number; lift: number }[] = [
  { color: "#d14234", from: 0, to: 8, drop: 0, lift: 0 },
  { color: "#f2a55f", from: 1, to: 7, drop: 5, lift: 1 },
  { color: "#e8dec5", from: 3, to: 5, drop: 9, lift: 0 },
]

const between = (lo: number, hi: number) => Math.floor(Math.random() * (hi - lo + 1)) + lo

function draw(canvas: HTMLCanvasElement) {
  const ctx = canvas.getContext("2d")
  if (!ctx) return
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  for (let left = 0; left < canvas.width; left += WIDTH) {
    for (const layer of LAYERS) {
      ctx.fillStyle = layer.color
      for (let i = layer.from; i < layer.to; i++) {
        const bottom = BASE[i] + layer.lift
        const top = between(MIN[i] - layer.drop, MAX[i] - layer.drop)
        // The pen draws upwards from the bottom edge; the canvas counts down.
        if (top > bottom) ctx.fillRect(left + i, ROWS - top, 1, top - bottom)
      }
    }
  }
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

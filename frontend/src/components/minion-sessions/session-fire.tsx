import { useEffect, useRef } from "react"
import { ROWS, burnFrame, newFire, type Fire } from "./session-fire-model"

// The pixel fire behind an On Fire row, painted from the heights
// session-fire-model.ts burns each frame. The look is the "CSS Pixel Fire
// Animation" pen's (https://codepen.io/anon/pen/REKKEM, MIT as public CodePen
// pens are): a canvas a few pixels tall scaled up crisp, redrawn a few times
// a second, each column a stack of red, orange and a pale core. The pen
// draws one flame; the line of fire and its flames are this code's own.
//
// The canvas is ROWS pixels tall whatever the row's height, and as wide as
// square pixels make it.

const FPS = 7

// The three layers, from the pen: red to the top, orange below it, the pale
// core only in the tall flames.
const RED = "#d14234"
const ORANGE = "#f2a55f"
const CORE = "#e8dec5"
const ORANGE_BELOW = 3
const CORE_BELOW = 7

const state = new WeakMap<HTMLCanvasElement, Fire>()

function draw(canvas: HTMLCanvasElement) {
  const ctx = canvas.getContext("2d")
  if (!ctx) return
  const width = canvas.width
  let fire = state.get(canvas)
  if (!fire) {
    fire = newFire()
    state.set(canvas, fire)
  }
  const heights = burnFrame(fire, width)

  ctx.clearRect(0, 0, width, ROWS)
  // Upwards from the bottom edge, as the pen draws; the canvas counts down.
  const column = (color: string, below: number) => {
    ctx.fillStyle = color
    for (let x = 0; x < width; x++) {
      const h = Math.min(ROWS, heights[x] - below)
      if (h > 0) ctx.fillRect(x, ROWS - h, 1, h)
    }
  }
  column(RED, 0)
  column(ORANGE, ORANGE_BELOW)
  column(CORE, CORE_BELOW)
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

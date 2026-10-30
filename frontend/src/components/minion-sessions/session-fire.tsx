import { useEffect, useRef } from "react"

// The pixel fire behind an On Fire row: a low line of fire along the whole
// row, with now and then a tall flame rising out of it and dying down.
// The look is the "CSS Pixel Fire Animation" pen's
// (https://codepen.io/anon/pen/REKKEM, MIT as public CodePen pens are): a
// canvas a few pixels tall scaled up crisp, redrawn a few times a second,
// each column a stack of red, orange and a pale core. The pen draws one
// flame; the line and its flames here are this file's own.
//
// The canvas is ROWS pixels tall whatever the row's height, and as wide as
// square pixels make it.

const FPS = 7
const ROWS = 20

// The line: each column BASE_MIN..BASE_MAX high, plus a slow wave rolling
// along the row so it does not just shimmer in place.
const BASE_MIN = 2
const BASE_MAX = 5
const WAVE = 1.5

// The flames: about this many alive across a row of FLAME_SPAN columns at
// once, each FLAME_HEIGHT high at its tallest, FLAME_HALF columns either side
// of its middle, living FLAME_LIFE frames (it grows, then dies down).
const FLAMES_ALIVE = 2
const FLAME_SPAN = 900
const FLAME_HEIGHT: [number, number] = [10, 16]
const FLAME_HALF: [number, number] = [3, 5]
const FLAME_LIFE: [number, number] = [4, 9]

// The three layers, from the pen: red to the top, orange below it, the pale
// core only in the tall flames.
const RED = "#d14234"
const ORANGE = "#f2a55f"
const CORE = "#e8dec5"
const ORANGE_BELOW = 3
const CORE_BELOW = 7

const between = (lo: number, hi: number) => Math.floor(Math.random() * (hi - lo + 1)) + lo

interface Flame {
  middle: number
  half: number
  height: number
  life: number
  age: number
}

interface Fire {
  flames: Flame[]
  frame: number
}

const state = new WeakMap<HTMLCanvasElement, Fire>()

function draw(canvas: HTMLCanvasElement) {
  const ctx = canvas.getContext("2d")
  if (!ctx) return
  const width = canvas.width
  let fire = state.get(canvas)
  if (!fire) {
    fire = { flames: [], frame: 0 }
    state.set(canvas, fire)
  }
  fire.frame++

  // Flames die down and new ones catch, at the rate that keeps about
  // FLAMES_ALIVE per FLAME_SPAN columns burning.
  fire.flames = fire.flames.filter((f) => ++f.age < f.life)
  const meanLife = (FLAME_LIFE[0] + FLAME_LIFE[1]) / 2
  if (Math.random() < ((FLAMES_ALIVE / meanLife) * width) / FLAME_SPAN) {
    fire.flames.push({
      middle: between(0, width - 1),
      half: between(...FLAME_HALF),
      height: between(...FLAME_HEIGHT),
      life: between(...FLAME_LIFE),
      age: 0,
    })
  }

  const heights = new Array<number>(width)
  for (let x = 0; x < width; x++) {
    heights[x] = between(BASE_MIN, BASE_MAX) + Math.round(WAVE * Math.sin(x * 0.35 - fire.frame * 0.8))
  }
  for (const f of fire.flames) {
    // Grows, then dies down; narrows to its tip; never quite still.
    const strength = Math.sin((Math.PI * (f.age + 1)) / (f.life + 1))
    for (let d = -f.half; d <= f.half; d++) {
      const x = f.middle + d
      if (x < 0 || x >= width) continue
      const taper = 1 - (d / (f.half + 1)) ** 2
      const h = Math.round(f.height * strength * taper) + between(-1, 1)
      heights[x] = Math.max(heights[x], h)
    }
  }

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

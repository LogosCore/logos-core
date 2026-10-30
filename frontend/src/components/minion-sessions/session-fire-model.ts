// The fire behind an On Fire row, frame by frame: how high each column of
// the canvas burns (SessionFire paints it). A low line of fire along the
// whole row, with now and then a tall flame rising out of it and dying down.
// Apart from the component so it can be tested, and fast refresh keeps
// working (a component's file exports components only).

export const ROWS = 20

// The line: tongues of fire every TONGUE columns, each BASE_MIN..BASE_MAX
// high at its tip and wandering up and down on its own, by up to STEP a
// frame. A tongue falls off SLOPE a column to either side, to a point; the
// line between them is never below FLOOR, and every column flickers a pixel.
// Nothing moves along the row: fire flickers where it burns. (A wave rolling
// along the row, as first drawn, read as fire flowing from left to right;
// smooth bumps, as next, as hills.)
export const FLOOR = 1
const BASE_MIN = 2
export const BASE_MAX = 7
const TONGUE = 6
const STEP = 1.2
const SLOPE = 1.2

// The flames: about this many alive across a row of FLAME_SPAN columns at
// once, each FLAME_HEIGHT high at its tallest, FLAME_HALF columns either side
// of its middle, living FLAME_LIFE frames (it grows, then dies down, swaying
// a column now and then).
const FLAMES_ALIVE = 2
const FLAME_SPAN = 900
const FLAME_HEIGHT: [number, number] = [10, 16]
const FLAME_HALF: [number, number] = [3, 5]
const FLAME_LIFE: [number, number] = [4, 9]

const between = (lo: number, hi: number) => Math.floor(Math.random() * (hi - lo + 1)) + lo

interface Flame {
  middle: number
  half: number
  height: number
  life: number
  age: number
}

/** A fire's state from frame to frame. */
export interface Fire {
  /** The tips of the line's tongues, one every TONGUE columns. */
  tongues: number[]
  flames: Flame[]
}

export const newFire = (): Fire => ({ tongues: [], flames: [] })

/** Burns a fire one frame on, across width columns: each column's height. */
export function burnFrame(fire: Fire, width: number): number[] {
  // The tongues wander, each on its own; a new width lights them afresh.
  const tongues = Math.ceil(width / TONGUE) + 1
  if (fire.tongues.length !== tongues) {
    fire.tongues = Array.from({ length: tongues }, () => BASE_MIN + Math.random() * (BASE_MAX - BASE_MIN))
  } else {
    fire.tongues = fire.tongues.map((t) => Math.min(BASE_MAX, Math.max(BASE_MIN, t + (Math.random() * 2 - 1) * STEP)))
  }

  // Flames die down and new ones catch, at the rate that keeps about
  // FLAMES_ALIVE per FLAME_SPAN columns burning.
  fire.flames = fire.flames.filter((f) => ++f.age < f.life)
  for (const f of fire.flames) if (Math.random() < 0.3) f.middle += Math.random() < 0.5 ? -1 : 1
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
    // The taller of the tongues either side, where it reaches.
    const i = Math.floor(x / TONGUE)
    const left = fire.tongues[i] - (x - i * TONGUE) * SLOPE
    const right = fire.tongues[i + 1] - ((i + 1) * TONGUE - x) * SLOPE
    heights[x] = Math.round(Math.max(left, right, FLOOR)) + between(0, 1)
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
  return heights
}

import { HEAT_BUMP, between, type Point, type Random } from "./prompt-fx-model"
import type { Rgba } from "./session-fire-model"

// Inferno, the prompt's second effect (prompt-fx-model.ts), worked out here
// apart from its canvas (prompt-fx-inferno.ts): fire as the On Fire mark
// burns it, the Doom fire (session-fire-model.ts), in its colors, on a grid
// of CELL-pixel cells along the bottom of the console, painted crisp; but
// quick, as light stuff burns: what catches flashes up in a small puff and
// is gone. Heat comes into it where things happen, and the fire rises,
// spreads and dies out on its own: a key typed puffs a flame at the caret,
// a little taller as the heat
// builds (keys coming fast), and while it is hot a bed of fire burns along
// the bottom, the taller the hotter; a word completed (Tab) or a line
// recalled (↑ ↓) burns along like a fuse; text pasted flames up along its
// letters; text cut burns briefly where it was, in smoke and ash; a line
// that runs burns all at once, its letters glowing hot and burning out as
// the fire roars up off them; a line that does not read is doused: steam,
// the fire and the heat halved.

// ---- Heat: how fast keys come, as lightning's charge (prompt-fx-model.ts)

export { HEAT_BUMP, HEAT_TAU, cooled, fed } from "./prompt-fx-model"

/** What a line that does not read leaves of the heat, and of the fire. */
export const DOUSE = 0.5

const clamp01 = (n: number) => Math.min(1, Math.max(0, n))

// ---- The fire

/** Its cells, in CSS pixels: about the On Fire mark's. */
export const CELL = 2
/** How high it reaches, in pixels: room over the line for its tallest puff. */
export const FIELD = 64
/** Heats run 0 (none) to FIRE_TOP, the mark's colors spread over them: what burns hottest flashes white. */
export const FIRE_TOP = 36
/**
 * Degrees heat loses a row as it rises, COOL_MIN to COOL_MAX at random: a
 * puff is up and gone in a few rows (the mark's slow fire loses one or so),
 * never higher than its heat over COOL_MIN.
 */
export const COOL_MIN = 2
export const COOL_MAX = 5
/** How often heat drifts a column to either side as it rises: a puff spreads. */
const DRIFT = 0.6
/** The bed's source at the top of the heat, and the heat it starts at. */
export const BED_TOP = 12
export const BED_FROM = 0.12

/** The bed's source heat for heat: none while it is cool. */
export function bedLevel(heat: number): number {
  return heat < BED_FROM ? 0 : Math.max(1, Math.round(heat * BED_TOP))
}

/** A column's bed source heat: as hot as the heat is, flickering. */
export function sourceLevel(heat: number, random: Random): number {
  const bed = bedLevel(heat)
  if (bed === 0) return 0
  return Math.max(0, bed - (random() < 0.35 ? 1 : 0) - (random() < 0.1 ? 2 : 0))
}

/** The fire's state from step to step. */
export interface Blaze {
  cols: number
  rows: number
  /** Row by row from the top, rows + 1 of them: the last is the bed's source, never drawn. */
  heat: Uint8Array
  /** The heats a step before: what painting blends from, as the On Fire mark does. */
  before: Uint8Array
  /** The columns in the order a step takes them: shuffled each step. */
  order: Uint32Array
}

export function newBlaze(cols: number, rows: number): Blaze {
  const size = (rows + 1) * cols
  return { cols, rows, heat: new Uint8Array(size), before: new Uint8Array(size), order: Uint32Array.from({ length: cols }, (_, x) => x) }
}

/** Sets the bed's source: each column's heat, 0 to FIRE_TOP. */
export function feed(b: Blaze, level: (x: number) => number) {
  const source = b.rows * b.cols
  for (let x = 0; x < b.cols; x++) b.heat[source + x] = Math.max(0, Math.min(FIRE_TOP, Math.round(level(x))))
}

/** Heat where something burns: columns x0 to x1 of row, at least level (the fire rises from there). */
export function kindle(b: Blaze, x0: number, x1: number, row: number, level: number) {
  const r = Math.max(0, Math.min(b.rows - 1, Math.round(row)))
  const l = Math.max(0, Math.min(FIRE_TOP, Math.round(level)))
  for (let x = Math.max(0, Math.round(x0)); x <= Math.min(b.cols - 1, Math.round(x1)); x++) {
    const i = r * b.cols + x
    if (b.heat[i] < l) b.heat[i] = l
  }
}

/** The fire doused: share of its heat left everywhere. */
export function douse(b: Blaze, share: number) {
  for (let i = 0; i < b.heat.length; i++) b.heat[i] = Math.floor(b.heat[i] * share)
}

/**
 * The fire a step on, as the On Fire mark burns it (session-fire-model.ts)
 * but quicker: heat rises a row, cooling COOL_MIN to COOL_MAX at random,
 * drifting a column either way at random; the columns in a fresh order each
 * step, so it leans neither way. What drifts onto a column not yet taken
 * rises again with it: the tongues.
 */
export function rise(b: Blaze, random: Random) {
  const { cols, rows, heat, order } = b
  for (let i = cols - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1))
    const t = order[i]
    order[i] = order[j]
    order[j] = t
  }
  for (const x of order) {
    for (let y = 1; y <= rows; y++) {
      const h = heat[y * cols + x]
      if (h === 0) {
        heat[(y - 1) * cols + x] = 0
        continue
      }
      const cool = COOL_MIN + Math.floor(random() * (COOL_MAX - COOL_MIN + 1))
      const d = random()
      const to = d < DRIFT / 2 ? Math.max(0, x - 1) : d < DRIFT ? Math.min(cols - 1, x + 1) : x
      heat[(y - 1) * cols + to] = Math.max(0, h - cool)
    }
  }
}

/** Whether any of the fire still burns above its bed's source. */
export function burning(b: Blaze): boolean {
  for (let i = 0; i < b.rows * b.cols; i++) if (b.heat[i] > 0) return true
  return false
}

/** The color of each heat, 0 to top, RGBA: 0 see-through, then along colors, the last at top. */
export function fireColors(colors: readonly Rgba[], top: number): Uint8ClampedArray {
  const stops: readonly Rgba[] = [[colors[0][0], colors[0][1], colors[0][2], 0], ...colors]
  const out = new Uint8ClampedArray((top + 1) * 4)
  for (let level = 0; level <= top; level++) {
    const p = (level / top) * (stops.length - 1)
    const k = Math.min(stops.length - 2, Math.floor(p))
    const u = p - k
    for (let j = 0; j < 4; j++) out[level * 4 + j] = Math.round(stops[k][j] + (stops[k + 1][j] - stops[k][j]) * u)
  }
  return out
}

/** A typed key's puff at heat: a source at the caret this hot, for this long, this many cells either side; a little taller, longer and wider as the heat builds. */
export function keyFlame(heat: number): { level: number; seconds: number; half: number } {
  const h = clamp01(heat)
  return { level: Math.round(18 + 10 * h), seconds: 0.04 + 0.04 * h, half: h >= 0.5 ? 2 : 1 }
}

// ---- Embers: sparks and ash (crisp), smoke and steam (soft)

export type EmberKind = "spark" | "smoke" | "steam" | "ash"

export interface Ember {
  kind: EmberKind
  x: number
  y: number
  /** Pixels a second. */
  vx: number
  vy: number
  /** Seconds. */
  life: number
  age: number
  /** Its size in pixels, at birth. */
  size: number
  /** How hot it is at birth, 0 to 1: a spark's color. */
  temp: number
}

/** A burst of embers: how many, how fast, how wide a cone around straight up (down: down), how long, how big, how hot. */
export interface EmberBurst {
  kind: EmberKind
  count: readonly [number, number]
  speed: readonly [number, number]
  /** Radians either side. */
  spread: number
  life: readonly [number, number]
  size: readonly [number, number]
  temp?: readonly [number, number]
  down?: boolean
}

export function embers(at: Point, b: EmberBurst, random: Random): Ember[] {
  const n = Math.min(b.count[1], Math.floor(between(random, b.count[0], b.count[1] + 1)))
  return Array.from({ length: n }, () => {
    const angle = (b.down ? Math.PI / 2 : -Math.PI / 2) + (random() * 2 - 1) * b.spread
    const speed = between(random, b.speed[0], b.speed[1])
    return {
      kind: b.kind,
      x: at.x,
      y: at.y,
      vx: Math.cos(angle) * speed,
      vy: Math.sin(angle) * speed,
      life: between(random, b.life[0], b.life[1]),
      age: 0,
      size: between(random, b.size[0], b.size[1]),
      temp: b.temp ? between(random, b.temp[0], b.temp[1]) : 0,
    }
  })
}

// How each kind moves: lift (pixels a second, each second, upward; ash's
// is gravity), drag (a share of its speed lost a second), jitter (a random
// push either way, pixels a second each second).
const MOTION: Record<EmberKind, { lift: number; drag: number; jitter: number }> = {
  spark: { lift: 60, drag: 0.8, jitter: 120 },
  smoke: { lift: 40, drag: 1.6, jitter: 60 },
  steam: { lift: 60, drag: 1.4, jitter: 70 },
  ash: { lift: -260, drag: 2.5, jitter: 160 },
}

/** An ember dt seconds on: lifted (or fallen), slowed, pushed about, older. */
export function stepEmber(e: Ember, dt: number, random: Random) {
  const m = MOTION[e.kind]
  e.vy -= m.lift * dt
  e.vx += (random() * 2 - 1) * m.jitter * dt
  const keep = Math.max(0, 1 - m.drag * dt)
  e.vx *= keep
  e.vy *= keep
  e.x += e.vx * dt
  e.y += e.vy * dt
  e.age += dt
}

/** How an ember looks at its age: a spark burns down, cooling; smoke and steam swell and thin out; ash fades. */
export function emberLook(e: Ember): { size: number; alpha: number; temp: number } {
  const t = Math.min(1, Math.max(0, e.age / e.life))
  switch (e.kind) {
    case "spark":
      return { size: e.size, alpha: 1 - t, temp: e.temp * (1 - 0.5 * t) }
    case "smoke":
      return { size: e.size * (1 + 2.2 * t), alpha: 0.3 * Math.min(1, t * 5) * (1 - t), temp: 0 }
    case "steam":
      return { size: e.size * (1 + 2.6 * t), alpha: 0.35 * Math.min(1, t * 5) * (1 - t), temp: 0 }
    case "ash":
      return { size: e.size, alpha: 0.85 * (1 - t), temp: 0 }
  }
}

// ---- Fuses: a flame that runs along a word or a line

export interface Fuse {
  from: Point
  to: Point
  /** Seconds. */
  duration: number
  age: number
}

/** Seconds a completed word's fuse takes along length pixels: quick, however long. */
export function fuseDuration(length: number): number {
  return Math.min(0.3, Math.max(0.08, length / 900))
}

/** Seconds a recalled line's fuse takes along length pixels: a little longer as it is longer, so a long line's reads as a sweep. */
export function recallDuration(length: number): number {
  return Math.min(0.3, Math.max(0.1, 0.1 + length / 4000))
}

const fuseLength = (f: Fuse) => Math.hypot(f.to.x - f.from.x, f.to.y - f.from.y)

/** How far along a fuse has burnt at its age, in pixels: all of it once its time is up. */
export function fuseAlong(f: Fuse): number {
  const length = fuseLength(f)
  return f.duration > 0 ? length * Math.min(1, Math.max(0, f.age / f.duration)) : length
}

/** The point d pixels along a fuse. */
export function pointAlong(f: Fuse, d: number): Point {
  const length = fuseLength(f)
  const t = length > 0 ? Math.min(1, Math.max(0, d / length)) : 0
  return { x: f.from.x + (f.to.x - f.from.x) * t, y: f.from.y + (f.to.y - f.from.y) * t }
}

/**
 * What a fuse throws as it burns along: one every `every` pixels, and at
 * least `least` along it, so a long line throws as many as its length and
 * a short word no fewer.
 */
export interface FuseThrow {
  spec: EmberBurst
  every: number
  least: number
}

/** Pixels between a throw's embers along a fuse length long; 0 when there is nothing to burn along (all are thrown at once). */
export function throwSpacing(t: FuseThrow, length: number): number {
  return length > 0 ? Math.min(t.every, length / t.least) : 0
}

/** The distances along a fuse where embers are due, from next up to upTo, spacing apart; and the one due after them. */
export function throwsUpTo(next: number, upTo: number, spacing: number): { at: number[]; next: number } {
  const at: number[] = []
  if (spacing <= 0) return { at, next }
  let d = next
  // A hair over: the last one lands at the end however the sums round.
  for (; d <= upTo + 1e-6; d += spacing) at.push(d)
  return { at, next: d }
}

// ---- A line burning, all at once

/**
 * How a burning letter looks t seconds after its line ran: as typed before
 * (original 1); glowing hot as it catches, over catches seconds (original
 * giving way to hot); burning out over burnsOut, cooling (temp, 1 to 0.3)
 * as it goes; then gone.
 */
export function letterLook(t: number, spec: { catches: number; burnsOut: number }): { original: number; hot: number; temp: number } {
  if (t < 0) return { original: 1, hot: 0, temp: 0 }
  if (t < spec.catches) {
    const u = t / spec.catches
    return { original: 1 - u, hot: u, temp: u }
  }
  const v = (t - spec.catches) / spec.burnsOut
  if (v < 1) return { original: 0, hot: 1 - v, temp: 1 - 0.7 * v }
  return { original: 0, hot: 0, temp: 0 }
}

// ---- What each happening kindles and throws, in pixels and seconds

const burst = (b: EmberBurst) => b

export const INFERNO = {
  typed: {
    heat: HEAT_BUMP,
    sparks: burst({ kind: "spark", count: [0, 1], speed: [30, 80], spread: 0.7, life: [0.15, 0.3], size: [1.2, 1.8], temp: [0.8, 1] }),
  },
  deleted: {
    heat: 0.03,
    smoke: burst({ kind: "smoke", count: [1, 2], speed: [8, 20], spread: 0.5, life: [0.35, 0.55], size: [2.5, 4] }),
    ash: burst({ kind: "ash", count: [2, 4], speed: [10, 30], spread: 1.3, life: [0.3, 0.5], size: [1.2, 1.6] }),
  },
  completed: {
    heat: 0.05,
    /** How hot its fuse burns; embers left glowing along the word. */
    level: 24,
    throws: [
      { spec: burst({ kind: "spark", count: [1, 1], speed: [0, 12], spread: 1.2, life: [0.15, 0.3], size: [1.2, 1.8], temp: [0.6, 0.9] }), every: 8, least: 4 },
    ] as readonly FuseThrow[],
  },
  recalled: {
    heat: 0.03,
    /** How hot its fuse burns (its time is the line's: recallDuration). Back to an older line, embers rise off it; on to a newer one, ash falls. */
    level: 22,
    older: [
      { spec: burst({ kind: "spark", count: [1, 1], speed: [20, 50], spread: 0.5, life: [0.15, 0.3], size: [1.2, 1.8], temp: [0.7, 1] }), every: 8, least: 4 },
    ] as readonly FuseThrow[],
    newer: [{ spec: burst({ kind: "ash", count: [1, 1], speed: [5, 20], spread: 1.2, life: [0.25, 0.45], size: [1.2, 1.6] }), every: 7, least: 4 }] as readonly FuseThrow[],
  },
  pasted: {
    /** What each letter adds to the heat, as a typed key does but less, and the most a paste adds. */
    heat: 0.04,
    most: 0.35,
    /** A flame off a letter every so many pixels along it, start to end, one after another (sweepDelay), this hot, this long. */
    every: 6,
    stagger: 0.01,
    within: 0.3,
    level: 22,
    seconds: 0.06,
  },
  cut: {
    heat: 0.03,
    every: 6,
    stagger: 0.008,
    within: 0.25,
    level: 16,
    seconds: 0.05,
    smoke: burst({ kind: "smoke", count: [0, 1], speed: [8, 20], spread: 0.5, life: [0.35, 0.55], size: [2.5, 4] }),
    ash: burst({ kind: "ash", count: [1, 2], speed: [10, 30], spread: 1.3, life: [0.3, 0.5], size: [1.2, 1.6] }),
  },
  burn: {
    heat: HEAT_BUMP,
    /** Seconds its letters take to catch, and then to burn out (letterLook), all at once: a flash. */
    catches: 0.05,
    burnsOut: 0.12,
    /** The puff off the whole line: white-hot, this long; a few sparks off random letters. */
    level: FIRE_TOP,
    seconds: 0.1,
    sparks: burst({ kind: "spark", count: [5, 9], speed: [30, 90], spread: 0.7, life: [0.15, 0.35], size: [1.2, 1.8], temp: [0.8, 1] }),
    /** The chance a letter burnt out leaves a wisp of smoke over it. */
    smokes: 0.25,
    smoke: burst({ kind: "smoke", count: [1, 1], speed: [8, 18], spread: 0.4, life: [0.35, 0.6], size: [2.5, 4] }),
  },
  refused: {
    steam: burst({ kind: "steam", count: [4, 6], speed: [15, 35], spread: 0.8, life: [0.4, 0.65], size: [3, 5] }),
    ash: burst({ kind: "ash", count: [2, 3], speed: [10, 30], spread: 1.2, life: [0.3, 0.5], size: [1.2, 1.6] }),
  },
}

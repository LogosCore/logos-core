// The console prompt's effects (the console's /fx, console-builtins.ts),
// worked out here apart from the canvas that draws them
// (prompt-fx-engine.ts), so they can be tested, and fast refresh keeps
// working. An edit that moves more than a letter (a paste, a cut) plays
// along the text it moved. Both effects grow with how fast keys come
// (heat, cooled): inferno's heat, lightning's charge.
// Lightning, the first: a small bolt strikes the caret as a key is
// typed, sparks fly off as one is deleted, an arc runs along a word as it
// is completed (Tab) and sweeps along a line recalled from the history (↑
// back to older, ↓ on to newer; past the oldest it fizzles), a line that
// runs is struck from the top of the console, and one that does not read
// fizzles. Inferno, the second, is fire (prompt-fx-inferno-model.ts).

export type PromptFxName = "lightning" | "inferno"

/** The effects, as /fx offers them. */
export const PROMPT_FX: readonly { name: PromptFxName; description: string }[] = [
  { name: "lightning", description: "a bolt as a key is typed, a strike as a line runs" },
  { name: "inferno", description: "a flame as a key is typed, a wall of fire as a line runs: the faster you type, the hotter" },
]

/** A stored effect as the console shows it: one this version does not know shows none. */
export function readPromptFx(name: string | null | undefined): PromptFxName | null {
  return PROMPT_FX.find((f) => f.name === name)?.name ?? null
}

/** 0 to 1, at random: Math.random, or seeded for a test. */
export type Random = () => number

/** A repeatable Random (mulberry32). */
export function seeded(seed: number): Random {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** A number from min to max, at random. */
export function between(random: Random, min: number, max: number): number {
  return min + (max - min) * random()
}

const clamp01 = (n: number) => Math.min(1, Math.max(0, n))

// ---- How fast keys come: inferno's heat, lightning's charge. Each key adds
// to it, and it cools (runs down) to about a third in HEAT_TAU seconds.

/** Seconds it takes to cool to about a third. */
export const HEAT_TAU = 1.2
/** What a typed key adds: ten keys a second keep it near the top, two a second at a quarter. */
export const HEAT_BUMP = 0.09

/** heat, seconds later. */
export function cooled(heat: number, seconds: number): number {
  return heat * Math.exp(-Math.max(0, seconds) / HEAT_TAU)
}

/** heat seconds later, with by more: 1 at most. */
export function fed(heat: number, seconds: number, by: number): number {
  return Math.min(1, cooled(heat, seconds) + by)
}

export interface Point {
  x: number
  y: number
}

/** An edit of the line: at start, removed letters went (of the line before), inserted came (of the line after). */
export interface Edit {
  start: number
  removed: number
  inserted: number
}

/**
 * What changed from before to after, the caret at caret after it: what
 * lies between their common start and end, placed where the caret says
 * (an insertion ends at it, a removal starts at it) when the text agrees,
 * which tells where a letter went into a run of the same letter.
 */
export function editOf(before: string, after: string, caret: number): Edit {
  const most = Math.min(before.length, after.length)
  let head = 0
  while (head < most && before[head] === after[head]) head++
  let tail = 0
  while (tail < most - head && before[before.length - 1 - tail] === after[after.length - 1 - tail]) tail++
  const removed = before.length - head - tail
  const inserted = after.length - head - tail
  const guess = caret - inserted
  const fits =
    guess >= 0 &&
    guess <= head &&
    before.slice(0, guess) === after.slice(0, guess) &&
    before.slice(guess + removed) === after.slice(guess + inserted)
  return { start: fits ? guess : head, removed, inserted }
}

/** Text laid out in the page (client coordinates): where each letter starts and where it ends (text.length + 1), its middle, the input's edges. */
export interface TextSpan {
  text: string
  xs: number[]
  y: number
  /** What lies past these was scrolled out of sight. */
  left: number
  right: number
}

/**
 * The letters of a span in sight, not blanks, at least `every` pixels
 * apart (the ones between skipped): where an effect lands along it, at
 * their middles, and how far along it each is, 0 to 1.
 */
export function spanLetters(span: TextSpan, every: number): { x: number; at: number }[] {
  const start = span.xs[0]
  const length = span.xs[span.text.length] - start
  const out: { x: number; at: number }[] = []
  let last = -Infinity
  for (let i = 0; i < span.text.length; ) {
    const ch = String.fromCodePoint(span.text.codePointAt(i) ?? 32)
    const next = i + ch.length
    const x = (span.xs[i] + span.xs[next]) / 2
    i = next
    if (/\s/.test(ch) || x < span.left || x > span.right || x - last < every) continue
    last = x
    out.push({ x, at: length > 0 ? (x - start) / length : 0 })
  }
  return out
}

/** When something at `at` (0 to 1) along a span goes, of count going one after another stagger seconds apart, all within most seconds. */
export function sweepDelay(at: number, count: number, stagger: number, most: number): number {
  return Math.max(0, Math.min(1, at)) * Math.min(most, count * stagger)
}

/**
 * A jagged path from a to b, as lightning goes: each segment's middle moved
 * aside at random, generations times, by decay as much each time (midpoint
 * displacement: by half, it wanders widely and crackles finely; by more,
 * its fine creases stay sharp). It has 2^generations + 1 points, a and b
 * the ends.
 */
export function jagged(a: Point, b: Point, generations: number, roughness: number, random: Random, decay = 0.5): Point[] {
  let points = [a, b]
  let offset = roughness * Math.hypot(b.x - a.x, b.y - a.y)
  for (let g = 0; g < generations; g++) {
    const next: Point[] = [points[0]]
    for (let i = 1; i < points.length; i++) {
      const p = points[i - 1]
      const q = points[i]
      const dx = q.x - p.x
      const dy = q.y - p.y
      const length = Math.hypot(dx, dy) || 1
      const shift = (random() * 2 - 1) * offset
      next.push({ x: (p.x + q.x) / 2 - (dy / length) * shift, y: (p.y + q.y) / 2 + (dx / length) * shift }, q)
    }
    points = next
    offset *= decay
  }
  return points
}

/** A bolt's shape: how finely it crackles, how far aside it wanders, how many branches fork off it. */
export interface BoltShape {
  generations: number
  roughness: number
  /** The fewest and most branches. */
  branches: readonly [number, number]
  /** How sharp its creases stay as they get finer (jagged's); a half by default. */
  decay?: number
}

export interface Bolt {
  main: Point[]
  /** Forks off the main path: each starts on one of its points and dies out on the way. */
  branches: Point[][]
}

/** A bolt from a to b, with its branches. */
export function bolt(a: Point, b: Point, shape: BoltShape, random: Random): Bolt {
  const main = jagged(a, b, shape.generations, shape.roughness, random, shape.decay)
  const count = Math.min(shape.branches[1], Math.floor(between(random, shape.branches[0], shape.branches[1] + 1)))
  const branches: Point[][] = []
  for (let k = 0; k < count; k++) {
    // From the upper three quarters, off the way the bolt goes there.
    const i = Math.floor(between(random, 0.15, 0.75) * (main.length - 1))
    const from = main[i]
    const ahead = main[Math.min(i + 2, main.length - 1)]
    const angle = Math.atan2(ahead.y - from.y, ahead.x - from.x) + (random() < 0.5 ? -1 : 1) * between(random, 0.35, 0.9)
    const length = Math.hypot(b.x - from.x, b.y - from.y) * between(random, 0.2, 0.5)
    const to = { x: from.x + Math.cos(angle) * length, y: from.y + Math.sin(angle) * length }
    branches.push(jagged(from, to, Math.max(1, shape.generations - 2), shape.roughness, random, shape.decay))
  }
  return { main, branches }
}

/** How bright something is over its life: [when, 0 to 1 of the life; brightness, 0 to 1], in order. */
export type Keys = readonly (readonly [number, number])[]

/** keys' brightness at t (0 to 1 of the life), the keys joined by straight lines. */
export function brightness(keys: Keys, t: number): number {
  if (t <= keys[0][0]) return keys[0][1]
  for (let i = 1; i < keys.length; i++) {
    const [t1, v1] = keys[i]
    if (t <= t1) {
      const [t0, v0] = keys[i - 1]
      return v0 + (v1 - v0) * ((t - t0) / (t1 - t0))
    }
  }
  return keys[keys.length - 1][1]
}

/** A typed key's bolt: lit at once, out soon. */
export const TYPED_KEYS: Keys = [
  [0, 1],
  [0.4, 0.85],
  [1, 0],
]

/** A strike flickers as lightning does, its channel lit again by a second stroke, then fades. */
export const STRIKE_KEYS: Keys = [
  [0, 1],
  [0.12, 1],
  [0.2, 0.15],
  [0.3, 1],
  [0.45, 0.55],
  [1, 0],
]

/** A completed word's arc crackles: lit, a flicker, lit again, out. */
export const COMPLETED_KEYS: Keys = [
  [0, 1],
  [0.25, 0.9],
  [0.38, 0.3],
  [0.5, 0.85],
  [1, 0],
]

/** One arc of a set crossing a line of text (arcSet). */
export interface ArcStyle {
  /** Pixels it wanders aside at most, and how sharp its creases stay as they get finer (jagged's decay). */
  wander: number
  decay: number
  /** Generations more or fewer than its length's (arcGenerations). */
  extra: number
  /** Its width and brightness, times the set's. */
  width: number
  alpha: number
  /** Pixels above (less than 0) or below the text's middle; seconds after the first. */
  dy: number
  after: number
  branches: readonly [number, number]
}

/**
 * The arcs that cross a line length pixels long: two, a third along a long
 * line, up to two more as the charge builds. The first is the brightest
 * and widest; each other its own, wilder or tighter, creased finer or
 * coarser, dimmer, thinner, a little above or below, a moment later, now
 * and then forked. A long line's wander further (half again at most).
 */
export function arcSet(length: number, charge: number, random: Random): ArcStyle[] {
  const count = Math.min(5, 2 + (length > 200 ? 1 : 0) + Math.round(clamp01(charge) * 2))
  const reach = Math.min(1.5, 0.8 + length / 800)
  return Array.from({ length: count }, (_, i): ArcStyle =>
    i === 0
      ? { wander: between(random, 5, 8) * reach, decay: 0.55, extra: 0, width: 1.2, alpha: 1, dy: 0, after: 0, branches: [0, 1] }
      : {
          wander: between(random, 3, 12) * reach,
          decay: between(random, 0.45, 0.7),
          extra: random() < 0.5 ? -1 : 1,
          width: between(random, 0.5, 1),
          alpha: between(random, 0.45, 0.85),
          dy: between(random, -4, 4),
          after: between(random, 0, 0.04),
          branches: random() < 0.4 ? [1, 2] : [0, 0],
        },
  )
}

/**
 * An arc of style crossing length pixels: as fine as its length makes it
 * (more or less by its style), wandering no further than its wander: its
 * first move is wander × (1 − decay), and all of them add up to less.
 */
export function arcShape(style: ArcStyle, length: number): BoltShape {
  return {
    generations: Math.max(3, Math.min(8, arcGenerations(length) + style.extra)),
    roughness: Math.min(0.35, (style.wander * (1 - style.decay)) / Math.max(1, length)),
    branches: style.branches,
    decay: style.decay,
  }
}

/**
 * The generations that keep an arc's stretches about 3 pixels long: a long
 * line's arc crackles as finely as a short word's (3 to 7: 9 to 129 points).
 */
export function arcGenerations(length: number): number {
  return Math.max(3, Math.min(7, Math.round(Math.log2(Math.max(1, length) / 3))))
}

/** Points every `every` pixels from a towards b, short of b: where an arc crackles along its way. */
export function alongPoints(a: Point, b: Point, every: number): Point[] {
  const length = Math.hypot(b.x - a.x, b.y - a.y)
  const out: Point[] = []
  if (every <= 0 || length === 0) return out
  for (let d = every; d < length; d += every) out.push({ x: a.x + ((b.x - a.x) * d) / length, y: a.y + ((b.y - a.y) * d) / length })
  return out
}

/** A recalled line's arc: lit as it sweeps along the line, then out. */
export const RECALLED_KEYS: Keys = [
  [0, 1],
  [0.35, 1],
  [1, 0],
]

/**
 * How many of a swept path's n points show at t (0 to 1 of its life), the
 * path drawn out over the first sweep of its life: two at least (its first
 * stretch), all once it is swept, or when it is not (sweep 0).
 */
export function sweptPoints(n: number, t: number, sweep: number): number {
  if (sweep <= 0 || t >= sweep) return n
  return Math.min(n, Math.max(2, Math.ceil((Math.max(0, t) / sweep) * (n - 1)) + 1))
}

/** What a line that does not read leaves of the charge: it fizzles out. */
export const DISCHARGE = 0.5

/**
 * A typed key's bolt at charge (0 to 1): one, a little taller, forked and
 * creased, with a few more sparks, the faster keys come. The charge itself
 * plays across the line (chargeArc), not piled on the key.
 */
export function chargedKey(charge: number): {
  shape: BoltShape
  rise: readonly [number, number]
  width: number
  life: number
  sparks: SparkBurst
} {
  const c = clamp01(charge)
  const t = LIGHTNING.typed
  const by = (n: number, k: number) => n * (1 + k * c)
  return {
    shape: { generations: t.shape.generations, roughness: by(t.shape.roughness, 0.25), branches: c < 0.5 ? [0, 1] : [1, 2], decay: 0.5 + 0.1 * c },
    rise: [by(t.rise[0], 0.3), by(t.rise[1], 0.3)],
    width: by(t.width, 0.3),
    life: t.life,
    sparks: { ...t.sparks, count: [Math.round(by(t.sparks.count[0], 0.6)), Math.round(by(t.sparks.count[1], 0.6))] },
  }
}

/** One bolt of a line's strike (strikeSet). */
export interface StrikeBolt {
  /** How far from where it lands it may start at the top, times the strike's drift. */
  drift: number
  width: number
  alpha: number
  /** Seconds after the first. */
  after: number
  branches: readonly [number, number]
  decay: number
}

/**
 * The bolts of a line's strike at charge: the main one, and one more (up
 * to three as the charge builds), each from its own place at the top,
 * thinner, dimmer, a moment later, creased its own way.
 */
export function strikeSet(charge: number, random: Random): StrikeBolt[] {
  const count = 2 + Math.round(clamp01(charge) * 2)
  return Array.from({ length: count }, (_, i): StrikeBolt =>
    i === 0
      ? { drift: 1, width: 1, alpha: 1, after: 0, branches: LIGHTNING.strike.shape.branches, decay: 0.55 }
      : {
          drift: between(random, 0.6, 1.8),
          width: between(random, 0.45, 0.8),
          alpha: between(random, 0.5, 0.85),
          after: between(random, 0.01, 0.07),
          branches: [0, 2],
          decay: between(random, 0.45, 0.7),
        },
  )
}

/** The charge the line starts to crackle at. */
export const LINE_CHARGE_FROM = 0.2

/** Arcs a second crackling across the line at charge: none until it is up a little, two then, sixteen at the top. */
export function lineChargeRate(charge: number): number {
  return charge < LINE_CHARGE_FROM ? 0 : 2 + (14 * (clamp01(charge) - LINE_CHARGE_FROM)) / (1 - LINE_CHARGE_FROM)
}

/**
 * Where an arc of the charge crackles across a line: a stretch of its text
 * in sight, anywhere along it, longer as the charge is higher (10 to 34
 * pixels, to 60 at the top); null when too little of it is in sight.
 */
export function chargeArc(span: TextSpan, charge: number, random: Random): { from: number; to: number } | null {
  const start = Math.max(span.left, span.xs[0])
  const end = Math.min(span.right, span.xs[span.text.length])
  const room = end - start
  if (room < 6) return null
  const length = Math.min(room, between(random, 10, 34 + 26 * clamp01(charge)))
  const from = between(random, start, end - length)
  return { from, to: from + length }
}

export interface Spark {
  x: number
  y: number
  /** Pixels a second. */
  vx: number
  vy: number
  /** Seconds. */
  life: number
  age: number
}

/** A burst of sparks: how many, how fast, how wide a cone around straight up (or down), how long each lives. */
export interface SparkBurst {
  count: readonly [number, number]
  speed: readonly [number, number]
  /** Radians either side of straight up (down: of straight down). */
  spread: number
  life: readonly [number, number]
  down?: boolean
}

export function burst(at: Point, b: SparkBurst, random: Random): Spark[] {
  const n = Math.min(b.count[1], Math.floor(between(random, b.count[0], b.count[1] + 1)))
  return Array.from({ length: n }, () => {
    const angle = (b.down ? Math.PI / 2 : -Math.PI / 2) + (random() * 2 - 1) * b.spread
    const speed = between(random, b.speed[0], b.speed[1])
    return { x: at.x, y: at.y, vx: Math.cos(angle) * speed, vy: Math.sin(angle) * speed, life: between(random, b.life[0], b.life[1]), age: 0 }
  })
}

/** Pixels a second, each second: sparks arc and fall. */
export const GRAVITY = 520

/** A spark dt seconds on: moved, fallen a little, older. */
export function stepSpark(s: Spark, dt: number) {
  s.vy += GRAVITY * dt
  s.x += s.vx * dt
  s.y += s.vy * dt
  s.age += dt
}

/**
 * Milliseconds between two flashes at least: a strike's flash is soft and
 * local, and still never comes three times a second, however fast lines
 * run (WCAG 2.3.1).
 */
export const FLASH_GAP = 400

/** Whether a strike at now may flash, the last flash at last (null: none yet). */
export function mayFlash(now: number, last: number | null): boolean {
  return last === null || now - last >= FLASH_GAP
}

/** Lightning, in CSS pixels and seconds: what each happening draws. */
export const LIGHTNING = {
  typed: {
    shape: { generations: 4, roughness: 0.32, branches: [0, 1] } as BoltShape,
    /** How high above the caret it starts, and how far to either side. */
    rise: [30, 54] as const,
    drift: 14,
    life: 0.2,
    width: 1.1,
    sparks: { count: [3, 5], speed: [60, 180], spread: 1.1, life: [0.22, 0.38] } as SparkBurst,
  },
  deleted: {
    sparks: { count: [4, 6], speed: [30, 110], spread: 1.4, life: [0.15, 0.3], down: true } as SparkBurst,
  },
  completed: {
    /** Its arcs are a set (arcSet), each shaped by the word's length (arcShape). Shorter than min, no arcs: sparks alone. */
    min: 6,
    life: 0.26,
    width: 1,
    sparks: { count: [4, 6], speed: [60, 200], spread: 1.2, life: [0.2, 0.4] } as SparkBurst,
    /** Sparks crackling along the way, every so many pixels: as many along a long word as its length. */
    crackle: { every: 30, sparks: { count: [1, 2], speed: [30, 110], spread: 1.2, life: [0.15, 0.3] } as SparkBurst },
  },
  recalled: {
    /** Its arcs are a set (arcSet), each shaped by the line's length (arcShape). Shorter than min (an empty line), no arcs: sparks alone. */
    min: 6,
    life: 0.3,
    /** The share of its life it takes to sweep along the line: its sparks fly when it gets there. */
    sweep: 0.35,
    width: 1,
    sparks: { count: [3, 5], speed: [50, 160], spread: 0.9, life: [0.18, 0.32] } as SparkBurst,
    /** Sparks crackling along the line as the sweep passes, every so many pixels: a long line as lively as a short one. */
    crackle: { every: 30, sparks: { count: [1, 2], speed: [30, 110], spread: 1.2, life: [0.15, 0.3] } as SparkBurst },
  },
  strike: {
    shape: { generations: 6, roughness: 0.26, branches: [2, 4] } as BoltShape,
    /** From the top of the console, this far to either side of where it lands. */
    drift: 90,
    life: 0.45,
    width: 1.8,
    sparks: { count: [12, 18], speed: [90, 320], spread: 1.35, life: [0.3, 0.6] } as SparkBurst,
    flash: { radius: 190, life: 0.3 },
  },
  refused: {
    sparks: { count: [8, 12], speed: [40, 150], spread: 1.5, life: [0.25, 0.45] } as SparkBurst,
  },
  /** The charge across the line (chargeArc): its arcs' wander, life and width; sparks popping off it, a second at the top. */
  charged: {
    wander: [2, 5] as const,
    life: [0.12, 0.22] as const,
    width: [0.6, 0.9] as const,
    sparkRate: 6,
    sparks: { count: [1, 2], speed: [30, 90], spread: 1, life: [0.15, 0.3] } as SparkBurst,
  },
  /** What a paste adds to the charge for each letter, and at most. */
  pasted: { charge: 0.04, most: 0.35 },
}

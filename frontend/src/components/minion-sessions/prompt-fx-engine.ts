import {
  COMPLETED_KEYS,
  DISCHARGE,
  HEAT_BUMP,
  LIGHTNING,
  RECALLED_KEYS,
  LINE_CHARGE_FROM,
  STRIKE_KEYS,
  TYPED_KEYS,
  alongPoints,
  arcSet,
  arcShape,
  between,
  bolt,
  brightness,
  burst,
  chargeArc,
  chargedKey,
  cooled,
  fed,
  lineChargeRate,
  mayFlash,
  stepSpark,
  strikeSet,
  sweptPoints,
  type ArcStyle,
  type Bolt,
  type Keys,
  type Point,
  type Spark,
  type SparkBurst,
  type TextSpan,
} from "./prompt-fx-model"

// The prompt's effects drawn: an engine for each (lightning here, inferno in
// prompt-fx-inferno.ts), on a canvas laid over a console (PromptFxLayer),
// told what the prompt does (FxEngine). Each draws only while something it
// drew still lives: a frame each screen refresh then, none at all when it
// is still, so a console with an effect on costs nothing between keys.

/** A line as it ran, laid out in the page (client coordinates): what an effect burns or strikes. */
export interface LineShape extends TextSpan {
  /** As the input draws it: its font (a canvas font), size in pixels, color. */
  font: string
  size: number
  color: string
  /** Where the text ends, inside the input. */
  end: Point
}

/** What a console's prompt tells its effect, at points in the page (client coordinates). */
export interface FxEngine {
  /** A key typed at the caret; deleted: one went. */
  typed(at: Point, deleted: boolean): void
  /** A word completed (Tab), from where it starts to where it ends. */
  completed(from: Point, to: Point): void
  /** A line recalled from the history, from where it starts to where it ends; older: ↑. */
  recalled(from: Point, to: Point, older: boolean): void
  /** More than a letter came at once (a paste, a drop, an undo): where it is now. */
  pasted(span: TextSpan): void
  /** More than a letter went at once (a cut, a selection deleted): where it was. */
  cut(span: TextSpan): void
  /** A line ran. */
  ran(line: LineShape): void
  /** A line that does not read, its end at; at the oldest line of the history, ↑. */
  refused(at: Point): void
  /** The line as it now stands, after an edit, a completion or a recall; null once it ran. */
  lineIs(span: TextSpan | null): void
  dispose(): void
}

/** A frame to draw: the time (performance.now()), seconds since the last, whether the page is dark, the canvas's size in CSS pixels. */
export interface Frame {
  now: number
  dt: number
  dark: boolean
  width: number
  height: number
}

/** An effect's canvas, as surface keeps it. */
export interface Surface {
  /** Its size in CSS pixels. */
  readonly width: number
  readonly height: number
  /** A point in the page as a point on the canvas. */
  local(at: Point): Point
  /** Draws from the next screen refresh on, while draw says something lives. */
  kick(): void
  dispose(): void
}

/**
 * A canvas to draw an effect on: sized to it and the screen's pixels as it
 * resizes, and drawn on each screen refresh, after kick, while draw says
 * something still lives. draw gets its context in CSS pixels, cleared,
 * painting over, fully opaque; a frame's dt is a twentieth of a second at
 * most, so a tab that was hidden does not jump.
 */
export function surface(canvas: HTMLCanvasElement, draw: (ctx: CanvasRenderingContext2D, f: Frame) => boolean): Surface {
  const ctx = canvas.getContext("2d")
  let width = 0
  let height = 0
  let ratio = 1
  const fit = () => {
    ratio = window.devicePixelRatio || 1
    width = canvas.clientWidth
    height = canvas.clientHeight
    canvas.width = Math.round(width * ratio)
    canvas.height = Math.round(height * ratio)
  }
  fit()
  const observer = new ResizeObserver(fit)
  observer.observe(canvas)
  let frame = 0
  let last = 0
  const tick = (now: number) => {
    frame = 0
    const dt = Math.min(0.05, Math.max(0, (now - last) / 1000))
    last = now
    if (!ctx) return
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0)
    ctx.globalCompositeOperation = "source-over"
    ctx.globalAlpha = 1
    ctx.clearRect(0, 0, width, height)
    if (draw(ctx, { now, dt, dark: canvas.closest(".dark") !== null, width, height })) frame = requestAnimationFrame(tick)
  }
  return {
    get width() {
      return width
    },
    get height() {
      return height
    },
    local(at) {
      const r = canvas.getBoundingClientRect()
      return { x: at.x - r.left, y: at.y - r.top }
    },
    kick() {
      if (frame !== 0) return
      last = performance.now()
      frame = requestAnimationFrame(tick)
    },
    dispose() {
      observer.disconnect()
      if (frame !== 0) cancelAnimationFrame(frame)
      frame = 0
      // Nothing left half drawn: the canvas may be another effect's next.
      ctx?.setTransform(1, 0, 0, 1, 0, 0)
      ctx?.clearRect(0, 0, canvas.width, canvas.height)
    },
  }
}

export type Rgb = readonly [number, number, number]

export const rgba = ([r, g, b]: Rgb, a: number) => `rgba(${r},${g},${b},${Math.max(0, Math.min(1, a))})`

// ---- Lightning (prompt-fx-model.ts): on a dark page the light adds up (a
// white-hot core in a blue glow); on a light one it is painted on, deep
// blue in a sky-blue glow, as light added to white shows nothing.

interface Palette {
  core: Rgb
  glow: Rgb
  outer: Rgb
  spark: Rgb
  refused: Rgb
  flash: Rgb
  /** How bright a flash is at its middle, at its brightest. */
  flashAlpha: number
  /** Whether light adds up (a dark page), or is painted on (a light one). */
  add: boolean
}

const DARK: Palette = {
  core: [255, 255, 255],
  glow: [147, 197, 253],
  outer: [99, 102, 241],
  spark: [186, 230, 253],
  refused: [251, 191, 36],
  flash: [147, 197, 253],
  flashAlpha: 0.2,
  add: true,
}

const LIGHT: Palette = {
  core: [30, 64, 175],
  glow: [59, 130, 246],
  outer: [125, 211, 252],
  spark: [37, 99, 235],
  refused: [217, 119, 6],
  flash: [56, 189, 248],
  flashAlpha: 0.12,
  add: false,
}

// A bolt is stroked three times, wide and faint to narrow and bright: the
// glow around it, then its core. [color, width times the bolt's, alpha].
const PASSES: readonly (readonly ["outer" | "glow" | "core", number, number])[] = [
  ["outer", 7, 0.12],
  ["glow", 3, 0.4],
  ["core", 1, 1],
]

// At most this many alive: keys held down, lines run fast, arcs in sets.
const MAX_BOLTS = 48
const MAX_SPARKS = 400

interface LiveBolt {
  main: Path2D
  /** A swept bolt's path, drawn out over the first sweep of its life; null: all at once. */
  points: Point[] | null
  sweep: number
  branches: Path2D[]
  /** Milliseconds (performance.now()): it strikes then. */
  born: number
  life: number
  width: number
  /** Its brightness, times its keys'. */
  alpha: number
  keys: Keys
}

/** How a bolt is drawn: its life (seconds), width and keys; swept over a share of its life, struck seconds after, dimmer by alpha. */
interface BoltLook {
  life: number
  width: number
  keys: Keys
  sweep?: number
  after?: number
  alpha?: number
}

/** Arcs across a line of text: their life, width and sparks; swept over a share of their life, or all at once. */
interface ArcSpec {
  min: number
  life: number
  width: number
  sparks: SparkBurst
  crackle: { every: number; sparks: SparkBurst }
  sweep?: number
}

interface LiveSpark extends Spark {
  refused: boolean
}

interface Flash {
  x: number
  y: number
  born: number
  life: number
  radius: number
}

function pathOf(points: readonly Point[]): Path2D {
  const p = new Path2D()
  p.moveTo(points[0].x, points[0].y)
  for (let i = 1; i < points.length; i++) p.lineTo(points[i].x, points[i].y)
  return p
}

/** A burst with more sparks as the charge (0 to 1) is higher: twice as many at the top. */
const more = (b: SparkBurst, charge: number): SparkBurst => ({
  ...b,
  count: [Math.round(b.count[0] * (1 + charge)), Math.round(b.count[1] * (1 + charge))],
})

/** Lightning on canvas. */
export function lightning(canvas: HTMLCanvasElement): FxEngine {
  const random = Math.random
  const bolts: LiveBolt[] = []
  const sparks: LiveSpark[] = []
  const flashes: Flash[] = []
  let lastFlash: number | null = null
  // The charge: how fast keys come (cooled), as inferno's heat. It
  // crackles across the line as it stands (on the canvas), not on the key.
  let charge = 0
  let chargeTime = performance.now()
  let line: TextSpan | null = null
  let arcCarry = 0
  let popCarry = 0
  const chargeAt = (now: number) => cooled(charge, (now - chargeTime) / 1000)
  const energize = (by: number) => {
    const now = performance.now()
    charge = fed(charge, (now - chargeTime) / 1000, by)
    chargeTime = now
  }

  const addBolt = (b: Bolt, look: BoltLook) => {
    const sweep = look.sweep ?? 0
    bolts.push({
      main: pathOf(b.main),
      points: sweep > 0 ? b.main : null,
      sweep,
      branches: b.branches.map(pathOf),
      born: performance.now() + (look.after ?? 0) * 1000,
      life: look.life * 1000,
      width: look.width,
      alpha: look.alpha ?? 1,
      keys: look.keys,
    })
    if (bolts.length > MAX_BOLTS) bolts.splice(0, bolts.length - MAX_BOLTS)
  }

  // after: seconds before they fly (a swept arc's, when it gets there).
  const addSparks = (at: Point, b: SparkBurst, refused: boolean, after = 0) => {
    for (const s of burst(at, b, random)) sparks.push({ ...s, refused, age: -after })
    if (sparks.length > MAX_SPARKS) sparks.splice(0, sparks.length - MAX_SPARKS)
  }

  // Arcs across a line of text, a little above its middle, from a to b
  // (where their sparks fly): a set (arcSet), each wandering, creased and
  // bright its own way, as finely along a long line as a short one, more
  // and livelier as the charge builds; none when it is too short, sparks
  // alone. Their sparks fall (down) or fly up.
  const arcsAlong = (a: Point, b: Point, spec: ArcSpec, keys: Keys, down: boolean) => {
    const c = chargeAt(performance.now())
    const from = { x: a.x, y: a.y - 2 }
    const to = { x: b.x, y: b.y - 2 }
    const length = Math.abs(to.x - from.x)
    const arc = length >= spec.min
    const sweep = spec.sweep ?? 0
    // A swept arc gets to a point in its share of the sweep.
    const sweepTime = arc ? spec.life * sweep : 0
    if (arc) {
      for (const style of arcSet(length, c, random)) {
        addBolt(bolt({ x: from.x, y: from.y + style.dy }, { x: to.x, y: to.y + style.dy }, arcShape(style, length), random), {
          life: spec.life,
          width: spec.width * style.width,
          keys,
          sweep,
          after: style.after,
          alpha: style.alpha,
        })
      }
      const crackle = { ...spec.crackle.sparks, down }
      for (const p of alongPoints(from, to, spec.crackle.every / (1 + 0.8 * c))) {
        addSparks(p, crackle, false, (sweepTime * Math.abs(p.x - from.x)) / length)
      }
    }
    addSparks(to, more({ ...spec.sparks, down }, c), false, sweepTime)
  }

  // The history's sweep: arcs drawn out along a line of text from a to b,
  // crackling as they go. A recalled line, and text pasted or cut, all go
  // the same way.
  const sweepAlong = (a: Point, b: Point, down: boolean) => arcsAlong(a, b, LIGHTNING.recalled, RECALLED_KEYS, down)

  // A span's ends on the canvas, as far as the input shows it.
  const spanEnds = (span: TextSpan): [Point, Point] => {
    const o = s.local({ x: 0, y: span.y })
    const from = Math.max(span.left, span.xs[0])
    const to = Math.min(span.right, span.xs[span.text.length])
    return [
      { x: from + o.x, y: o.y },
      { x: Math.max(from, to) + o.x, y: o.y },
    ]
  }

  const s = surface(canvas, (ctx, f) => {
    const p = f.dark ? DARK : LIGHT
    const c = chargeAt(f.now)

    // The charge, crackling across the line: arcs over stretches of its
    // text, sparks popping off it, the more and the longer the higher it is.
    const rate = line ? lineChargeRate(c) : 0
    if (rate === 0) {
      arcCarry = 0
      popCarry = 0
    } else if (line) {
      const ch = LIGHTNING.charged
      for (arcCarry += rate * f.dt; arcCarry >= 1; arcCarry--) {
        const arc = chargeArc(line, c, random)
        if (!arc) break
        const style: ArcStyle = {
          wander: between(random, ch.wander[0], ch.wander[1]),
          decay: 0.6,
          extra: 0,
          width: 1,
          alpha: 1,
          dy: 0,
          after: 0,
          branches: c >= 0.7 ? [0, 1] : [0, 0],
        }
        const y = line.y - 2 + (random() * 2 - 1) * 3
        addBolt(bolt({ x: arc.from, y }, { x: arc.to, y: y + (random() * 2 - 1) * 2 }, arcShape(style, arc.to - arc.from), random), {
          life: between(random, ch.life[0], ch.life[1]),
          width: between(random, ch.width[0], ch.width[1]),
          keys: COMPLETED_KEYS,
          alpha: 0.3 + 0.5 * c,
        })
      }
      for (popCarry += ch.sparkRate * c * f.dt; popCarry >= 1; popCarry--) {
        const at = chargeArc(line, 0, random)
        if (at) addSparks({ x: at.from, y: line.y - 3 }, ch.sparks, false)
      }
    }

    ctx.globalCompositeOperation = p.add ? "lighter" : "source-over"
    ctx.lineCap = "round"
    ctx.lineJoin = "round"

    for (let i = flashes.length - 1; i >= 0; i--) {
      const fl = flashes[i]
      // A frame's time can be a little before a flash was born: from 0.
      const t = Math.max(0, (f.now - fl.born) / fl.life)
      if (t >= 1) {
        flashes.splice(i, 1)
        continue
      }
      const g = ctx.createRadialGradient(fl.x, fl.y, 0, fl.x, fl.y, fl.radius)
      g.addColorStop(0, rgba(p.flash, p.flashAlpha * (1 - t) ** 2))
      g.addColorStop(1, rgba(p.flash, 0))
      ctx.fillStyle = g
      ctx.fillRect(fl.x - fl.radius, fl.y - fl.radius, fl.radius * 2, fl.radius * 2)
    }

    for (let i = bolts.length - 1; i >= 0; i--) {
      const b = bolts[i]
      const t = (f.now - b.born) / b.life
      if (t >= 1) {
        bolts.splice(i, 1)
        continue
      }
      // Not struck yet.
      if (t < 0) continue
      const alpha = brightness(b.keys, t) * b.alpha
      // A swept bolt drawn as far as it has got.
      const main = b.points && t < b.sweep ? pathOf(b.points.slice(0, sweptPoints(b.points.length, t, b.sweep))) : b.main
      for (const [color, wide, a] of PASSES) {
        const col = p[color]
        ctx.strokeStyle = rgba(col, alpha * a)
        ctx.lineWidth = b.width * wide
        ctx.stroke(main)
        // Branches thinner and dimmer, dying out.
        ctx.strokeStyle = rgba(col, alpha * a * 0.75)
        ctx.lineWidth = b.width * wide * 0.55
        for (const branch of b.branches) ctx.stroke(branch)
      }
    }

    ctx.lineWidth = 1.3
    for (let i = sparks.length - 1; i >= 0; i--) {
      const sp = sparks[i]
      // Not flying yet: waiting for its arc to get there.
      if (sp.age < 0) {
        sp.age += f.dt
        continue
      }
      stepSpark(sp, f.dt)
      if (sp.age >= sp.life) {
        sparks.splice(i, 1)
        continue
      }
      // A streak, as long as it flies in a fortieth of a second.
      ctx.strokeStyle = rgba(sp.refused ? p.refused : p.spark, 1 - sp.age / sp.life)
      ctx.beginPath()
      ctx.moveTo(sp.x - sp.vx * 0.025, sp.y - sp.vy * 0.025)
      ctx.lineTo(sp.x, sp.y)
      ctx.stroke()
    }

    return bolts.length + sparks.length + flashes.length > 0 || (line !== null && c >= LINE_CHARGE_FROM)
  })

  return {
    typed(at, deleted) {
      if (s.width === 0) return
      const o = s.local(at)
      if (deleted) {
        energize(0.03)
        addSparks(o, more(LIGHTNING.deleted.sparks, 0.5 * chargeAt(performance.now())), false)
      } else {
        energize(HEAT_BUMP)
        // One bolt, a little more as the charge builds: the charge itself
        // crackles across the line.
        const k = chargedKey(chargeAt(performance.now()))
        const to = { x: o.x, y: o.y - 3 }
        const from = { x: o.x + (random() * 2 - 1) * LIGHTNING.typed.drift, y: o.y - between(random, k.rise[0], k.rise[1]) }
        addBolt(bolt(from, to, k.shape, random), { life: k.life, width: k.width, keys: TYPED_KEYS })
        addSparks(to, k.sparks, false)
      }
      s.kick()
    },
    pasted(span) {
      if (s.width === 0) return
      // A paste charges as typing its letters would, at most so much.
      const letters = span.text.match(/\S/g)?.length ?? 0
      energize(Math.min(LIGHTNING.pasted.most, LIGHTNING.pasted.charge * letters))
      // Swept along, start to end, as a recalled line is; its sparks fly
      // up, as a typed key's do.
      const [a, b] = spanEnds(span)
      sweepAlong(a, b, false)
      s.kick()
    },
    cut(span) {
      if (s.width === 0) return
      // Swept along where it was, start to end; its sparks fall, as a
      // deleted key's do.
      const [a, b] = spanEnds(span)
      sweepAlong(a, b, true)
      s.kick()
    },
    completed(fromAt, toAt) {
      if (s.width === 0) return
      arcsAlong(s.local(fromAt), s.local(toAt), LIGHTNING.completed, COMPLETED_KEYS, false)
      s.kick()
    },
    recalled(fromAt, toAt, older) {
      if (s.width === 0) return
      // Back to an older line it sweeps back along it, end to start, its
      // sparks flying up; on to a newer one, start to end, falling.
      const [a, b] = older ? [s.local(toAt), s.local(fromAt)] : [s.local(fromAt), s.local(toAt)]
      sweepAlong(a, b, !older)
      s.kick()
    },
    ran(line) {
      if (s.width === 0) return
      energize(HEAT_BUMP)
      const c = chargeAt(performance.now())
      const st = LIGHTNING.strike
      const o = s.local(line.end)
      const to = { x: o.x, y: o.y - 3 }
      // A strike in bolts (strikeSet): the main one, and more as the charge
      // builds, each from its own place at the top, creased its own way.
      for (const sb of strikeSet(c, random)) {
        const from = { x: Math.min(s.width - 8, Math.max(8, o.x + (random() * 2 - 1) * st.drift * sb.drift)), y: 0 }
        addBolt(bolt(from, to, { ...st.shape, branches: sb.branches, decay: sb.decay }, random), {
          life: st.life,
          width: st.width * sb.width,
          keys: STRIKE_KEYS,
          after: sb.after,
          alpha: sb.alpha,
        })
      }
      addSparks(to, more(st.sparks, c), false)
      const now = performance.now()
      if (mayFlash(now, lastFlash)) {
        flashes.push({ x: to.x, y: to.y, born: now, life: st.flash.life * 1000, radius: st.flash.radius })
        lastFlash = now
      }
      s.kick()
    },
    refused(at) {
      if (s.width === 0) return
      // It fizzles: the charge halves.
      const now = performance.now()
      charge = chargeAt(now) * DISCHARGE
      chargeTime = now
      addSparks(s.local(at), LIGHTNING.refused.sparks, true)
      s.kick()
    },
    lineIs(span) {
      if (!span) {
        line = null
        return
      }
      const o = s.local({ x: 0, y: span.y })
      line = { text: span.text, xs: span.xs.map((x) => x + o.x), y: o.y, left: span.left + o.x, right: span.right + o.x }
      s.kick()
    },
    dispose: () => s.dispose(),
  }
}

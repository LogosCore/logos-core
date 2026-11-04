import { rgba, surface, type FxEngine, type LineShape, type Rgb } from "./prompt-fx-engine"
import {
  CELL,
  DOUSE,
  FIELD,
  FIRE_TOP,
  INFERNO,
  bedLevel,
  burning,
  cooled,
  douse,
  emberLook,
  embers,
  fed,
  feed,
  fireColors,
  fuseAlong,
  fuseDuration,
  keyFlame,
  kindle,
  letterLook,
  newBlaze,
  pointAlong,
  recallDuration,
  rise,
  sourceLevel,
  stepEmber,
  throwSpacing,
  throwsUpTo,
  type Blaze,
  type Ember,
  type EmberBurst,
  type Fuse,
  type FuseThrow,
} from "./prompt-fx-inferno-model"
import { spanLetters, sweepDelay, type Point } from "./prompt-fx-model"
import { FIRE_DARK, FIRE_LIGHT } from "./session-fire-model"

// Draws the prompt's inferno (prompt-fx-inferno-model.ts) on its console's
// canvas: the fire, the Doom fire along the bottom of the console on cells
// of CELL pixels, painted crisp as the On Fire mark's, its steps blended as
// they are painted so it moves smoothly; heat comes into it where things
// happen (sources), and it rises and dies out on its own. A line burning
// is its text drawn again where the input showed it (the input is empty by
// then), on top of the fire; sparks and ash are crisp specks, smoke and
// steam soft. On a dark page the fire's light adds up; on a light one it is
// painted on. It draws while anything burns, the bed too: from the first
// warm key until the heat has gone.

// The fire's steps a second, blended as they are painted: quick, a puff up
// and gone in a fifth of a second (the mark burns slower); at most this
// many embers.
const STEPS = 60
const MAX_EMBERS = 600
// A soft glow's size in canvas pixels (smoke, steam), drawn scaled.
const SPRITE = 32

/** A soft round glow of rgb: opaque at its middle, gone at its edge. */
function glow([r, g, b]: Rgb): HTMLCanvasElement {
  const c = document.createElement("canvas")
  c.width = SPRITE
  c.height = SPRITE
  const ctx = c.getContext("2d")
  if (ctx) {
    const half = SPRITE / 2
    const gradient = ctx.createRadialGradient(half, half, 0, half, half, half)
    gradient.addColorStop(0, rgba([r, g, b], 1))
    gradient.addColorStop(0.4, rgba([r, g, b], 0.55))
    gradient.addColorStop(1, rgba([r, g, b], 0))
    ctx.fillStyle = gradient
    ctx.fillRect(0, 0, SPRITE, SPRITE)
  }
  return c
}

/** How the inferno looks on a page, dark or light. */
interface Look {
  dark: boolean
  /** The fire's color for each heat (fireColors): the mark's. */
  colors: Uint8ClampedArray
  smoke: HTMLCanvasElement
  steam: HTMLCanvasElement
  ash: Rgb
}

function lookOf(dark: boolean): Look {
  return {
    dark,
    colors: fireColors(dark ? FIRE_DARK : FIRE_LIGHT, FIRE_TOP),
    smoke: glow(dark ? [110, 110, 110] : [70, 70, 70]),
    steam: glow(dark ? [215, 220, 225] : [140, 148, 156]),
    ash: dark ? [130, 130, 130] : [60, 60, 60],
  }
}

/** The fire's color at heat level (0 to FIRE_TOP), alpha times its own. */
function colorAt(l: Look, level: number, alpha: number): string {
  const o = Math.max(0, Math.min(FIRE_TOP, Math.round(level))) * 4
  return rgba([l.colors[o], l.colors[o + 1], l.colors[o + 2]], alpha * (l.colors[o + 3] / 255))
}

/** Where heat comes into the fire: its cells x0 to x1 of row, at least level, from until (performance.now()). */
interface Source {
  x0: number
  x1: number
  row: number
  level: number
  from: number
  until: number
}

interface LiveFuse extends Fuse {
  level: number
  /** How far along it has kindled the fire, in pixels. */
  burnt: number
  /** Each throw: its embers' spacing along this fuse, and where its next is due. */
  throws: { spec: EmberBurst; spacing: number; next: number }[]
}

/** A letter of a burning line, on the canvas. */
interface Letter {
  ch: string
  x: number
  width: number
}

interface Burn {
  letters: Letter[]
  y: number
  font: string
  size: number
  color: string
  /** When it ran (performance.now()): all its letters catch then. */
  born: number
  smoked: boolean
}

/** The inferno on canvas. */
export function inferno(canvas: HTMLCanvasElement): FxEngine {
  const random = Math.random
  let heat = 0
  let heatTime = performance.now()
  const all: Ember[] = []
  const fuses: LiveFuse[] = []
  const burns: Burn[] = []
  const sources: Source[] = []
  let fire: Blaze | null = null
  let steps = 0
  let look: Look | null = null
  const buffer = document.createElement("canvas")
  const bufferCtx = buffer.getContext("2d")
  let image: ImageData | null = null

  const heatAt = (now: number) => cooled(heat, (now - heatTime) / 1000)
  const warm = (by: number) => {
    const now = performance.now()
    heat = fed(heat, (now - heatTime) / 1000, by)
    heatTime = now
  }
  // after: seconds before they fly (a paste's, a cut's, one letter after another).
  const add = (list: Ember[], after = 0) => {
    for (const e of list) {
      e.age = -after
      all.push(e)
    }
    if (all.length > MAX_EMBERS) all.splice(0, all.length - MAX_EMBERS)
  }

  /** The fire for a canvas width by height: the console's width, FIELD high at most; made again when that changed. */
  const fireFor = (width: number, height: number): Blaze | null => {
    const cols = Math.ceil(width / CELL)
    const rows = Math.ceil(Math.min(FIELD, height) / CELL)
    if (cols <= 0 || rows <= 0) return null
    if (!fire || fire.cols !== cols || fire.rows !== rows) fire = newBlaze(cols, rows)
    return fire
  }
  /** The fire's row at y, on a canvas height high. */
  const rowAt = (b: Blaze, y: number, height: number) => Math.floor((y - (height - b.rows * CELL)) / CELL)

  /** Heat into the fire from x0 to x1 along a line of text whose middle is at y: at level, for seconds, after seconds. */
  const kindleAt = (x0: number, x1: number, y: number, level: number, seconds: number, after = 0) => {
    const b = fireFor(s.width, s.height)
    if (!b) return
    const from = performance.now() + after * 1000
    sources.push({ x0: Math.floor(x0 / CELL), x1: Math.floor(x1 / CELL), row: rowAt(b, y + 4, s.height), level, from, until: from + seconds * 1000 })
  }

  const fuse = (from: Point, to: Point, duration: number, level: number, throws: readonly FuseThrow[]) => {
    const length = Math.hypot(to.x - from.x, to.y - from.y)
    fireFor(s.width, s.height)
    fuses.push({ from, to, duration, age: 0, level, burnt: -1, throws: throws.map((t) => ({ spec: t.spec, spacing: throwSpacing(t, length), next: 0 })) })
  }

  /** What a fuse has newly burnt along since its last step: kindled, a run of cells, so its flame is whole however fast it runs. */
  const kindleFuse = (b: Blaze, fu: LiveFuse, height: number) => {
    const upTo = fuseAlong(fu)
    if (upTo <= fu.burnt) return
    const a = pointAlong(fu, Math.max(0, fu.burnt))
    const c = pointAlong(fu, upTo)
    kindle(b, Math.floor(Math.min(a.x, c.x) / CELL), Math.floor(Math.max(a.x, c.x) / CELL), rowAt(b, c.y + 4, height), fu.level - Math.floor(random() * 3))
    fu.burnt = upTo
  }

  /** The fire painted crisp along the bottom, blended between its last two steps at at (0 to 1). */
  const paintFire = (ctx: CanvasRenderingContext2D, b: Blaze, l: Look, height: number, at: number) => {
    if (!bufferCtx) return
    if (!image || image.width !== b.cols || image.height !== b.rows) {
      image = new ImageData(b.cols, b.rows)
      buffer.width = b.cols
      buffer.height = b.rows
    }
    const px = image.data
    const { heat: now, before, cols, rows } = b
    const colors = l.colors
    for (let i = 0; i < cols * rows; i++) {
      const o = Math.round(before[i] + (now[i] - before[i]) * at) * 4
      const p = i * 4
      px[p] = colors[o]
      px[p + 1] = colors[o + 1]
      px[p + 2] = colors[o + 2]
      px[p + 3] = colors[o + 3]
    }
    bufferCtx.putImageData(image, 0, 0)
    ctx.imageSmoothingEnabled = false
    ctx.drawImage(buffer, 0, height - rows * CELL, cols * CELL, rows * CELL)
  }

  const drawGlow = (ctx: CanvasRenderingContext2D, sprite: HTMLCanvasElement, x: number, y: number, radius: number, alpha: number) => {
    if (alpha <= 0 || radius <= 0) return
    ctx.globalAlpha = Math.min(1, alpha)
    ctx.drawImage(sprite, x - radius, y - radius, radius * 2, radius * 2)
  }

  const s = surface(canvas, (ctx, f) => {
    if (look?.dark !== f.dark) look = lookOf(f.dark)
    const l = look
    const h = heatAt(f.now)
    const bed = bedLevel(h)
    const spec = INFERNO.burn

    for (let i = sources.length - 1; i >= 0; i--) if (f.now > sources[i].until) sources.splice(i, 1)

    // Fuses burn along; what they throw, where it falls due.
    for (const fu of fuses) {
      fu.age += f.dt
      const upTo = fuseAlong(fu)
      for (const t of fu.throws) {
        const due = throwsUpTo(t.next, upTo, t.spacing)
        t.next = due.next
        for (const d of due.at) {
          const p = pointAlong(fu, d)
          add(embers({ x: p.x + (random() * 2 - 1) * 1.5, y: p.y }, t.spec, random))
        }
      }
    }

    // The fire: the bed feeding it from below, the sources and fuses where
    // things happen, a step at a time.
    const feeding = bed > 0 || sources.length > 0 || fuses.length > 0
    const b = feeding ? fireFor(f.width, f.height) : fire
    if (b) {
      steps = Math.min(3, steps + f.dt * STEPS)
      for (; steps >= 1; steps--) {
        b.before.set(b.heat)
        feed(b, () => sourceLevel(h, random))
        rise(b, random)
        // A source flickers a degree or two.
        for (const src of sources) if (f.now >= src.from) kindle(b, src.x0, src.x1, src.row, src.level - Math.floor(random() * 3))
        for (const fu of fuses) kindleFuse(b, fu, f.height)
      }
      if (!feeding && !burning(b)) fire = null
    }
    for (let i = fuses.length - 1; i >= 0; i--) if (fuses[i].age >= fuses[i].duration && fuses[i].burnt >= fuseAlong(fuses[i])) fuses.splice(i, 1)

    // Lines burning: their letters out, some leave smoke.
    for (let i = burns.length - 1; i >= 0; i--) {
      const bn = burns[i]
      if ((f.now - bn.born) / 1000 < spec.catches + spec.burnsOut) continue
      if (!bn.smoked) {
        bn.smoked = true
        for (const lt of bn.letters) {
          if (random() < spec.smokes) add(embers({ x: lt.x + lt.width / 2, y: bn.y - bn.size * 0.5 }, spec.smoke, random))
        }
      }
      burns.splice(i, 1)
    }

    for (let i = all.length - 1; i >= 0; i--) {
      const e = all[i]
      // Not flying yet: waiting for its letter's turn.
      if (e.age < 0) {
        e.age += f.dt
        continue
      }
      stepEmber(e, f.dt, random)
      if (e.age >= e.life) all.splice(i, 1)
    }

    // Smoke and steam under the fire, painted on.
    for (const e of all) {
      if ((e.kind !== "smoke" && e.kind !== "steam") || e.age < 0) continue
      const k = emberLook(e)
      drawGlow(ctx, e.kind === "smoke" ? l.smoke : l.steam, e.x, e.y, k.size * 2, k.alpha)
    }
    // The fire and its sparks: their light adds up on a dark page.
    ctx.globalAlpha = 1
    ctx.globalCompositeOperation = f.dark ? "lighter" : "source-over"
    if (fire) paintFire(ctx, fire, l, f.height, steps)
    for (const e of all) {
      if (e.kind !== "spark" || e.age < 0) continue
      const k = emberLook(e)
      // A crisp speck that flickers.
      ctx.fillStyle = colorAt(l, k.temp * FIRE_TOP, k.alpha * (0.6 + 0.4 * random()))
      ctx.fillRect(e.x - k.size / 2, e.y - k.size / 2, k.size, k.size)
    }
    // The burning letters over their fire, and ash, painted on.
    ctx.globalCompositeOperation = "source-over"
    ctx.textBaseline = "middle"
    for (const bn of burns) {
      const k = letterLook((f.now - bn.born) / 1000, spec)
      ctx.font = bn.font
      for (const lt of bn.letters) {
        if (k.original > 0) {
          ctx.globalAlpha = k.original
          ctx.fillStyle = bn.color
          ctx.fillText(lt.ch, lt.x, bn.y)
        }
        if (k.hot > 0) {
          // On a light page no hotter than orange: yellow on white does not show.
          ctx.globalAlpha = 1
          ctx.fillStyle = colorAt(l, Math.min(k.temp, f.dark ? 1 : 0.7) * FIRE_TOP, k.hot)
          ctx.fillText(lt.ch, lt.x, bn.y)
        }
      }
    }
    ctx.globalAlpha = 1
    for (const e of all) {
      if (e.kind !== "ash" || e.age < 0) continue
      const k = emberLook(e)
      ctx.fillStyle = rgba(l.ash, k.alpha)
      ctx.fillRect(e.x - k.size / 2, e.y - k.size / 2, k.size, k.size)
    }

    return all.length > 0 || fuses.length > 0 || burns.length > 0 || sources.length > 0 || fire !== null
  })

  return {
    typed(at, deleted) {
      if (s.width === 0) return
      const o = s.local(at)
      if (deleted) {
        warm(INFERNO.deleted.heat)
        add(embers(o, INFERNO.deleted.smoke, random))
        add(embers(o, INFERNO.deleted.ash, random))
      } else {
        warm(INFERNO.typed.heat)
        // A flame at the caret, as tall and as long as the heat makes it.
        const k = keyFlame(heatAt(performance.now()))
        kindleAt(o.x - k.half * CELL, o.x + k.half * CELL, o.y, k.level, k.seconds)
        add(embers({ x: o.x, y: o.y + 3 }, INFERNO.typed.sparks, random))
      }
      s.kick()
    },
    pasted(span) {
      if (s.width === 0) return
      // As if its letters were typed at once: flames off them start to
      // end, one after another; the paste heats as much.
      const p = INFERNO.pasted
      const letters = spanLetters(span, p.every)
      warm(Math.min(p.most, p.heat * letters.length))
      const o = s.local({ x: 0, y: span.y })
      for (const lt of letters) {
        const x = lt.x + o.x
        kindleAt(x - CELL, x + CELL, o.y, p.level, p.seconds, sweepDelay(lt.at, letters.length, p.stagger, p.within))
      }
      s.kick()
    },
    cut(span) {
      if (s.width === 0) return
      // Burnt away where it was: each letter briefly in flame, smoke and
      // falling ash, one after another.
      const c = INFERNO.cut
      warm(c.heat)
      const letters = spanLetters(span, c.every)
      const o = s.local({ x: 0, y: span.y })
      for (const lt of letters) {
        const at = { x: lt.x + o.x, y: o.y }
        const after = sweepDelay(lt.at, letters.length, c.stagger, c.within)
        kindleAt(at.x - CELL, at.x + CELL, at.y, c.level, c.seconds, after)
        add(embers(at, c.smoke, random), after)
        add(embers(at, c.ash, random), after)
      }
      s.kick()
    },
    completed(fromAt, toAt) {
      if (s.width === 0) return
      const c = INFERNO.completed
      warm(c.heat)
      const a = s.local(fromAt)
      const b = s.local(toAt)
      fuse(a, b, fuseDuration(Math.abs(b.x - a.x)), c.level, c.throws)
      s.kick()
    },
    recalled(fromAt, toAt, older) {
      if (s.width === 0) return
      const r = INFERNO.recalled
      warm(r.heat)
      // Back to an older line it burns back along it, end to start, embers
      // rising; on to a newer one, start to end, ash falling.
      const [a, b] = older ? [s.local(toAt), s.local(fromAt)] : [s.local(fromAt), s.local(toAt)]
      fuse(a, b, recallDuration(Math.abs(b.x - a.x)), r.level, older ? r.older : r.newer)
      s.kick()
    },
    ran(line: LineShape) {
      if (s.width === 0 || line.text === "") return
      const spec = INFERNO.burn
      warm(spec.heat)
      const now = performance.now()
      // The line on the canvas, where the input showed it: its letters in
      // sight (what the input had scrolled away burns unseen).
      const o = s.local({ x: 0, y: 0 })
      const letters: Letter[] = []
      for (let i = 0; i < line.text.length; ) {
        const ch = String.fromCodePoint(line.text.codePointAt(i) ?? 32)
        const next = i + ch.length
        const x = line.xs[i] + o.x
        const width = line.xs[next] - line.xs[i]
        i = next
        if (/\s/.test(ch) || x + width < line.left + o.x || x > line.right + o.x) continue
        letters.push({ ch, x, width })
      }
      if (letters.length === 0) return
      const y = line.y + o.y
      // All at once: every letter catches as the fire roars up off the line.
      burns.push({ letters, y, font: line.font, size: line.size, color: line.color, born: now, smoked: false })
      const last = letters[letters.length - 1]
      kindleAt(letters[0].x, last.x + last.width, y, spec.level, spec.seconds)
      for (let n = Math.floor(spec.sparks.count[0] + random() * (spec.sparks.count[1] - spec.sparks.count[0] + 1)); n > 0; n--) {
        const lt = letters[Math.floor(random() * letters.length)]
        add(embers({ x: lt.x + lt.width / 2, y: y - line.size * 0.3 }, { ...spec.sparks, count: [1, 1] }, random))
      }
      s.kick()
    },
    refused(at) {
      if (s.width === 0) return
      // Doused: steam; the heat, the fire and what feeds it put out by half.
      const now = performance.now()
      heat = heatAt(now) * DOUSE
      heatTime = now
      if (fire) douse(fire, DOUSE)
      sources.length = 0
      const o = s.local(at)
      add(embers(o, INFERNO.refused.steam, random))
      add(embers(o, INFERNO.refused.ash, random))
      s.kick()
    },
    // The line as it stands is lightning's to crackle across; the heat
    // shows in the bed.
    lineIs() {},
    dispose: () => s.dispose(),
  }
}

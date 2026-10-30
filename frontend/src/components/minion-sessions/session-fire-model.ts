// The fire behind an On Fire row, frame by frame: the classic Doom fire (PSX
// Doom; see https://fabiensanglard.net/doom_fire_psx/), tuned for a row.
// A grid of heat, ROWS high: each frame the heat of every pixel rises a row,
// cooling a little at random and drifting a column to either side at random,
// so flames lick up, sway, break off and fade, as fire does. Heat comes from
// a source row under the grid (never drawn): a low bed of fire along the
// whole row, its heat varying along it, with now and then a hot spot that
// throws up a tall flame for a moment. SessionFire paints the heats.
//
// Apart from the component so it can be tested, and fast refresh keeps
// working (a component's file exports components only).

export const ROWS = 20
/** Heats run 0 (none) to LEVELS - 1. */
export const LEVELS = 14

// The bed: its heat, BED_MIN..BED_MAX, wanders along the row in stretches of
// BED_STRETCH columns, each on its own; a column now and then burns a
// degree lower. About 5 rows of fire, under the text: a hotter bed (8 rows)
// burned through the letters' lower half.
const BED_MIN = 4
const BED_MAX = 6
const BED_STRETCH = 6

// The hot spots: HOT, 2-4 columns either side of their middle, burning
// HOT_LIFE frames; about one alive every HOT_SPACING columns. Their flames
// reach two thirds of the way up and more.
export const HOT = 12
const HOT_HALF: [number, number] = [2, 4]
const HOT_LIFE: [number, number] = [6, 16]
const HOT_SPACING = 135

// Rising a row, heat cools 0 (COOL_0 of the time), 1 (COOL_1) or 2 (the
// rest), and drifts a column left or right DRIFT of the time (either way
// alike: no wind).
const COOL_0 = 0.3
const COOL_1 = 0.45
const DRIFT = 0.4

const between = (lo: number, hi: number) => Math.floor(Math.random() * (hi - lo + 1)) + lo

interface Spot {
  middle: number
  half: number
  life: number
}

/** A fire's state from frame to frame. */
export interface Fire {
  width: number
  /** Row by row from the top, ROWS + 1 of them: the last is the source. */
  heat: Uint8Array
  /** The bed's heat in each stretch of the row. */
  beds: number[]
  spots: Spot[]
  /** The columns in the order a frame takes them: shuffled each frame. */
  order: Uint32Array
}

/** A fire width columns wide, lit and burning. */
export function newFire(width: number): Fire {
  const fire: Fire = {
    width,
    heat: new Uint8Array((ROWS + 1) * width),
    beds: Array.from({ length: Math.ceil(width / BED_STRETCH) }, () => between(BED_MIN, BED_MAX)),
    spots: [],
    order: Uint32Array.from({ length: width }, (_, x) => x),
  }
  // Long enough for the heat to reach the top: it shows burning, not catching.
  for (let i = 0; i < ROWS * 2; i++) burnFrame(fire)
  return fire
}

/** Burns a fire one frame on. */
export function burnFrame(fire: Fire): void {
  const { width, heat, order } = fire
  const source = ROWS * width

  // The bed wanders, a degree at a time now and then; hot spots catch and
  // burn out.
  fire.beds = fire.beds.map((b) => Math.min(BED_MAX, Math.max(BED_MIN, b + [-1, 0, 0, 1][between(0, 3)])))
  const meanLife = (HOT_LIFE[0] + HOT_LIFE[1]) / 2
  if (Math.random() < width / HOT_SPACING / meanLife) {
    fire.spots.push({ middle: between(0, width - 1), half: between(...HOT_HALF), life: between(...HOT_LIFE) })
  }
  fire.spots = fire.spots.filter((s) => s.life-- > 0)
  for (let x = 0; x < width; x++) {
    heat[source + x] = fire.beds[Math.floor(x / BED_STRETCH)] - (Math.random() < 0.2 ? 1 : 0)
  }
  for (const s of fire.spots) {
    for (let x = Math.max(0, s.middle - s.half); x <= Math.min(width - 1, s.middle + s.half); x++) heat[source + x] = HOT
  }

  // Up a row, in place, as Doom does: what drifts onto a column not yet
  // taken this frame rises again with it, which makes the tongues. Doom
  // takes the columns left to right, so heat drifting right climbs faster
  // and the fire leans; a fresh random order each frame leans neither way.
  for (let i = width - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    const t = order[i]
    order[i] = order[j]
    order[j] = t
  }
  for (const x of order) {
    for (let y = 1; y <= ROWS; y++) {
      const h = heat[y * width + x]
      const r = Math.random()
      const cool = r < COOL_0 ? 0 : r < COOL_0 + COOL_1 ? 1 : 2
      const d = Math.random()
      const to = d < DRIFT / 2 ? Math.max(0, x - 1) : d < DRIFT ? Math.min(width - 1, x + 1) : x
      if (h === 0) heat[(y - 1) * width + x] = 0
      else heat[(y - 1) * width + to] = Math.max(0, h - cool)
    }
  }
}

/** How high each column burns: the top of its flame, in rows. */
export function flameHeights(fire: Fire): number[] {
  const { width, heat } = fire
  return Array.from({ length: width }, (_, x) => {
    for (let y = 0; y < ROWS; y++) if (heat[y * width + x] > 0) return ROWS - y
    return 0
  })
}

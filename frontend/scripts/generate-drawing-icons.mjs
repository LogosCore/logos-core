#!/usr/bin/env node
// Regenerates src/components/wiki/drawing/drawing-icons.generated.ts, and
// copies it and the shape catalogue (drawing-catalogue.ts) into the sidecar,
// which builds the same shapes for agents (hocuspocus/src/diagram-*.ts) and
// cannot import from this package: its image is built from hocuspocus/ alone.
// drawing-catalogue.test.ts fails if the copies drift.
//
// The built-in drawing library (drawing-builtin-library.ts) puts a lucide icon
// on every card. Excalidraw has no icon element, and an image would need its
// own storage, export and print plumbing (a library item cannot carry image
// files). So each icon is turned into Excalidraw's own shapes instead:
// ellipses, rectangles and lines. That keeps it editable, recolourable and
// visible everywhere a drawing is rendered, including the server's
// .excalidraw export.
//
// lucide draws on a 24×24 grid with 2px strokes, the same kind of line
// Excalidraw draws, which is why this conversion holds up. Curves and arcs
// are sampled into short straight segments, fine enough to look smooth at
// card size.
//
// It reads the INSTALLED lucide-react, so it needs no network. The output is
// committed; run it after a lucide bump or when adding an icon to ICONS:
//
//   npm run gen:drawing-icons
//
// drawing-library.test.ts fails if the library uses an icon this file lacks.

import { copyFileSync, readFileSync, writeFileSync } from "node:fs"
import { createRequire } from "node:module"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = join(dirname(fileURLToPath(import.meta.url)), "..")
const require = createRequire(import.meta.url)
const { parsePath, absolutize, normalize } = await import(
  join(root, "node_modules/path-data-parser/lib/index.js")
)
const ICONS_DIR = join(root, "node_modules/lucide-react/dist/esm/icons")
const DRAWING_DIR = join(root, "src/components/wiki/drawing")
const OUT = join(DRAWING_DIR, "drawing-icons.generated.ts")
const SIDECAR_DIR = join(root, "../hocuspocus/src")
const { version } = require(join(root, "node_modules/lucide-react/package.json"))

// Every icon the built-in shapes use, and the icons an agent may name with
// `icon=` in the diagram language (hocuspocus/src/diagram-language.ts). A
// curated few hundred rather than all of lucide: each one is converted to
// Excalidraw shapes and shipped in the canvas chunk and the sidecar, so the
// set is what a network map or an attack path actually needs. Kept here
// rather than read from the library, so this script needs no TypeScript.
const ICONS = [
  ...new Set(
    `
    app-window ban brick-wall bug cloud cpu crosshair crown database ethernet-port
    eye flag folder-open globe hash key-round laptop lock mail monitor network
    radio-tower router server shield-check skull smartphone terminal
    triangle-alert user user-cog venetian-mask

    server-cog server-crash server-off database-zap database-backup hard-drive
    hard-drive-download memory-stick monitor-smartphone tablet printer wifi
    wifi-off radio antenna satellite satellite-dish cable plug unplug
    globe-lock earth cloud-cog cloud-download cloud-upload cloud-off container
    box boxes package layers layers-2 workflow git-branch git-fork git-merge code
    code-xml square-terminal braces binary bot webhook panel-top layout-dashboard
    lock-open lock-keyhole unlock key key-square fingerprint-pattern shield
    shield-alert shield-x shield-off shield-user shield-half shield-ban
    brick-wall-fire flame siren eye-off scan scan-eye scan-face radar target
    bug-off ghost hat-glasses user-check user-x user-plus user-minus user-round
    users users-round contact id-card badge-check building building-2 factory
    warehouse landmark house store school hospital mail-warning mail-open inbox
    send message-square message-circle phone phone-call voicemail at-sign rss
    file file-text file-code file-key file-lock file-warning file-search
    file-archive files folder folder-lock archive clipboard clipboard-list
    notebook book book-open scroll tag tags link link-2 unlink paperclip qr-code
    barcode circle-alert octagon-alert info circle-help circle-x circle-check
    check x flag-triangle-right trophy star zap bolt activity gauge timer clock
    calendar hourglass history download upload refresh-cw rotate-ccw
    arrow-right-left repeat shuffle split merge filter search search-code
    settings settings-2 wrench hammer cog sliders-horizontal toggle-left power
    power-off battery-charging dollar-sign credit-card wallet banknote coins
    receipt shopping-cart map map-pin compass navigation route signpost camera
    cctv video mic headphones speaker car truck plane ship train-front bike
    chart-bar chart-line chart-pie trending-up trending-down lightbulb puzzle
    gem anchor magnet atom brain heart-pulse stethoscope syringe pill fish worm
    biohazard radiation ticket vote gavel scale handshake briefcase
    `
      .split(/\s+/)
      .filter(Boolean),
  ),
].sort()

/** The `__iconNode` array of one lucide-react icon module. Read as data, not
 * imported: importing pulls in React. */
/** The module that actually draws an icon. Some names are aliases kept for
 * compatibility ("circle-help" re-exports "circle-question-mark"); the alias
 * is the name offered, the target holds the drawing. */
function canonical(name) {
  const src = readFileSync(join(ICONS_DIR, `${name}.mjs`), "utf8")
  const alias = src.match(/export \{ default \} from '\.\/([a-z0-9-]+)\.mjs'/)
  return alias ? canonical(alias[1]) : name
}

function iconNode(name) {
  const src = readFileSync(join(ICONS_DIR, `${canonical(name)}.mjs`), "utf8")
  const match = src.match(/const __iconNode = (\[[\s\S]*?\]);\n/)
  if (!match) throw new Error(`${name}: no __iconNode`)
  return JSON.parse(match[1].replace(/([{,]\s*)([A-Za-z][\w-]*):/g, '$1"$2":'))
}

const r2 = (n) => Math.round(n * 100) / 100
const num = (v) => Number.parseFloat(v)

/** Points along a cubic, excluding its start. Denser on longer curves. */
function sampleCubic(p0, p1, p2, p3) {
  const approx =
    Math.hypot(p1[0] - p0[0], p1[1] - p0[1]) +
    Math.hypot(p2[0] - p1[0], p2[1] - p1[1]) +
    Math.hypot(p3[0] - p2[0], p3[1] - p2[1])
  const steps = Math.min(24, Math.max(3, Math.ceil(approx / 0.8)))
  const out = []
  for (let i = 1; i <= steps; i++) {
    const t = i / steps
    const u = 1 - t
    out.push([
      u * u * u * p0[0] + 3 * u * u * t * p1[0] + 3 * u * t * t * p2[0] + t * t * t * p3[0],
      u * u * u * p0[1] + 3 * u * u * t * p1[1] + 3 * u * t * t * p2[1] + t * t * t * p3[1],
    ])
  }
  return out
}

/** A run of points as a line, or as a dot when it has no length. lucide
 * draws dots as zero-length strokes with round caps, which Excalidraw would
 * not draw at all. */
function polyline(points, closed) {
  const pts = points.map(([x, y]) => [r2(x), r2(y)])
  if (closed) pts.push(pts[0])
  let length = 0
  for (let i = 1; i < pts.length; i++) length += Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1])
  if (length < 0.5) return { k: "d", x: pts[0][0], y: pts[0][1] }
  return { k: "l", p: pts }
}

/**
 * The same path with every number separated. SVG lets an arc's two flags run
 * into the next number ("a2 2 0 012 2" is flags 0 and 1, then x 2), and
 * path-data-parser reads "012" as one number. lucide's paths are minified
 * that way, so arcs are re-spaced here: the fourth and fifth parameter of an
 * arc are one character each.
 */
function spacePath(d) {
  const out = []
  let i = 0
  let cmd = ""
  let index = 0
  const NUMBER = /^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?/
  while (i < d.length) {
    const ch = d[i]
    if (/[\s,]/.test(ch)) {
      i++
    } else if (/[A-Za-z]/.test(ch)) {
      cmd = ch
      index = 0
      out.push(ch)
      i++
    } else if ((cmd === "a" || cmd === "A") && (index % 7 === 3 || index % 7 === 4)) {
      out.push(ch)
      index++
      i++
    } else {
      const m = d.slice(i).match(NUMBER)
      if (!m) throw new Error(`bad path near "${d.slice(i, i + 10)}"`)
      // Pairs after a move's first are line-tos ("m1 1 2 2" draws a line).
      // path-data-parser repeats the move instead, which draws nothing, so
      // the line-to is written out.
      if ((cmd === "m" || cmd === "M") && index === 2) {
        cmd = cmd === "m" ? "l" : "L"
        index = 0
        out.push(cmd)
      }
      out.push(m[0])
      index++
      i += m[0].length
    }
  }
  return out.join(" ")
}

function convertPath(d) {
  d = spacePath(d)
  const out = []
  let current = []
  let start = null
  const end = (closed) => {
    if (current.length > 0) out.push(polyline(current, closed && current.length > 2))
    current = []
  }
  for (const seg of normalize(absolutize(parsePath(d)))) {
    const at = current.length > 0 ? current[current.length - 1] : start
    if (seg.key === "M") {
      end(false)
      start = [seg.data[0], seg.data[1]]
      current = [start]
    } else if (seg.key === "L") {
      if (current.length === 0) current = [at]
      current.push([seg.data[0], seg.data[1]])
    } else if (seg.key === "C") {
      if (current.length === 0) current = [at]
      const [x1, y1, x2, y2, x, y] = seg.data
      current.push(...sampleCubic(at, [x1, y1], [x2, y2], [x, y]))
    } else if (seg.key === "Z") {
      end(true)
      current = []
    }
  }
  end(false)
  return out
}

function convert(name) {
  const shapes = []
  for (const [tag, a] of iconNode(name)) {
    if (tag === "circle") {
      const r = num(a.r)
      shapes.push({ k: "e", x: r2(num(a.cx) - r), y: r2(num(a.cy) - r), w: r2(2 * r), h: r2(2 * r) })
    } else if (tag === "ellipse") {
      const rx = num(a.rx)
      const ry = num(a.ry)
      shapes.push({ k: "e", x: r2(num(a.cx) - rx), y: r2(num(a.cy) - ry), w: r2(2 * rx), h: r2(2 * ry) })
    } else if (tag === "rect") {
      shapes.push({
        k: "r",
        x: r2(num(a.x ?? 0)),
        y: r2(num(a.y ?? 0)),
        w: r2(num(a.width)),
        h: r2(num(a.height)),
        round: num(a.rx ?? a.ry ?? 0) > 0,
      })
    } else if (tag === "line") {
      shapes.push(polyline([[num(a.x1), num(a.y1)], [num(a.x2), num(a.y2)]], false))
    } else if (tag === "polyline" || tag === "polygon") {
      const pts = a.points.trim().split(/[\s,]+/).map(num)
      const pairs = []
      for (let i = 0; i + 1 < pts.length; i += 2) pairs.push([pts[i], pts[i + 1]])
      shapes.push(polyline(pairs, tag === "polygon"))
    } else if (tag === "path") {
      shapes.push(...convertPath(a.d))
    } else {
      throw new Error(`${name}: unsupported <${tag}>`)
    }
  }
  return shapes
}

const icons = Object.fromEntries(ICONS.map((name) => [name, convert(name)]))

// lucide's search keywords for these icons, from the copy the icon picker
// already keeps (lucide-tags.generated.ts), so the diagram language can
// answer an unknown icon name with icons that mean it: "vault" suggests
// lock and key, not "valve".
const TAGS_FILE = join(root, "src/components/wiki/lucide-tags.generated.ts")
const tagSource = readFileSync(TAGS_FILE, "utf8")
const pascal = (name) => name.split("-").map((p) => p[0].toUpperCase() + p.slice(1)).join("")
const tags = Object.fromEntries(
  ICONS.map((name) => {
    for (const key of [pascal(name), pascal(canonical(name))]) {
      const m = tagSource.match(new RegExp(`\\n  ${key}: (\\[[^\\n]*\\]),`))
      if (m) return [name, JSON.parse(m[1])]
    }
    return [name, []]
  }),
)

writeFileSync(
  OUT,
  `// Generated by scripts/generate-drawing-icons.mjs from lucide-react ${version}
// (ISC licence). Do not edit; run \`npm run gen:drawing-icons\`.
//
// Each icon is a list of shapes on lucide's 24×24 grid: "e" an ellipse, "r" a
// rectangle, "l" a line through points, "d" a dot.

export type IconShape =
  | { k: "e"; x: number; y: number; w: number; h: number }
  | { k: "r"; x: number; y: number; w: number; h: number; round: boolean }
  | { k: "l"; p: [number, number][] }
  | { k: "d"; x: number; y: number }

export const DRAWING_ICONS: Record<string, IconShape[]> = ${JSON.stringify(icons)}

/** lucide's search keywords for each icon, for suggesting icons by meaning. */
export const DRAWING_ICON_TAGS: Record<string, string[]> = ${JSON.stringify(tags)}
`,
)
console.log(`Wrote ${ICONS.length} icons to ${OUT}`)

for (const file of ["drawing-icons.generated.ts", "drawing-catalogue.ts"]) {
  copyFileSync(join(DRAWING_DIR, file), join(SIDECAR_DIR, file))
  console.log(`Copied ${file} to the sidecar`)
}

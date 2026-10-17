// The built-in drawing shapes: cards, badges, zones, connections and notes.
//
// SHARED FILE. The frontend's copy (src/components/wiki/drawing/) is the one
// to edit; hocuspocus/src/drawing-catalogue.ts is a byte-identical copy, made
// by `npm run gen:drawing-icons` in frontend/ and checked by
// drawing-catalogue.test.ts there. Two packages use it:
//
//   - the Shapes panel on the canvas (drawing-shapes-panel.tsx), which shows
//     every shape and inserts the one an operator picks;
//   - the agents' diagram language (hocuspocus/src/diagram-*.ts), which
//     builds the same cards and zones from a few lines of text, and reads
//     them back as text.
//
// So it imports nothing but the generated icons: no Excalidraw, no React,
// nothing either package lacks. Its output is Excalidraw *skeletons*: plain
// objects in the shape of Excalidraw's documented builder input
// (convertToExcalidrawElements), which the canvas converts, and which the
// sidecar converts with its own element normaliser.
//
// Every part of every shape carries a tag in `customData.logos`: which shape
// it belongs to and which part of it it is. Excalidraw keeps customData
// through every edit, copy and save, so a drawing read later can still tell
// a domain controller card from twenty unrelated lines and two texts. That
// is what lets an agent read a drawing as a diagram (diagram-read.ts).
//
// Drawings here are network maps, attack paths and infrastructure sketches
// (see core/pkg/mcp/skillassets/drawings.md). These shapes give them one
// visual language, so a map one operator drew reads the same to the next:
//
//   - cards: an icon tile, a title and a detail line ("DC01", "10.0.5.10 ·
//     Windows 2019"), coloured by role: infrastructure, identity, network,
//     offence, loot, finding;
//   - status badges that sit on a card's corner;
//   - severity chips on the usual critical-to-info scale, and legends;
//   - connections, zones, a step marker and a note.
//
// Most shapes come in two styles. Sketch is Excalidraw's hand-drawn look, for
// working diagrams. Clean has straight lines and Nunito, for diagrams that go
// into a client report. Icons are lucide's, converted into lines and ellipses
// by frontend/scripts/generate-drawing-icons.mjs, and are always drawn with
// straight lines: a 28px icon drawn rough turns into scribble.

import { DRAWING_ICONS, DRAWING_ICON_TAGS } from "./drawing-icons.generated.js"

// ---------------------------------------------------------------------------
// Types

/** One element as Excalidraw's builder takes it. Deliberately loose: the
 * frontend casts it to Excalidraw's own skeleton type, the sidecar
 * normalises it, and neither package can see the other's types. */
export interface Skeleton {
  type: "rectangle" | "ellipse" | "diamond" | "text" | "line" | "arrow"
  x: number
  y: number
  [key: string]: unknown
}

export type ShapeStyle = "sketch" | "clean"

/** What every part of a built-in shape says about itself, in
 * `customData.logos`. */
export interface ShapeTag {
  /** card:<kind>, zone:<kind>, badge:<key>, edge:<style>, note, step,
   * severity:<id>, legend:<name>. */
  shape: string
  /** frame, tile, icon, title, detail, label, line, or badge:<key>. */
  part: string
  style?: ShapeStyle
  /** The diagram id an agent gave this shape ("dc01"). Absent on shapes an
   * operator placed from the panel; their element id stands in. */
  node?: string
  /** A colour an agent chose (a palette name or #rrggbb), when it overrides
   * the shape's own. */
  color?: string
  /** A lucide icon an agent chose, when it overrides the shape's own. */
  icon?: string
}

export function tagOf(element: object): ShapeTag | null {
  const logos = ((element as { customData?: unknown }).customData as { logos?: unknown } | undefined)?.logos
  if (!logos || typeof logos !== "object") return null
  const tag = logos as Partial<ShapeTag>
  return typeof tag.shape === "string" && typeof tag.part === "string" ? (tag as ShapeTag) : null
}

/** Tag every element of a shape. The tag goes on each part, not only the
 * frame: an operator can ungroup a card, and each part should still say
 * what it was. */
function tagged(elements: Skeleton[], tag: Omit<ShapeTag, "part">, parts?: string[]): Skeleton[] {
  return elements.map((el, i) => {
    const part = parts?.[i] ?? (el.customData as { part?: string } | undefined)?.part ?? "frame"
    const out: Skeleton = { ...el, customData: { logos: { ...tag, part } } }
    const label = el.label as Record<string, unknown> | undefined
    if (label) out.label = { ...label, customData: { logos: { ...tag, part: "label" } } }
    return out
  })
}

/** A part marker for an element whose part is not "frame", read by tagged(). */
function part(el: Skeleton, name: string): Skeleton {
  return { ...el, customData: { part: name } }
}

// ---------------------------------------------------------------------------
// Styles

interface Style {
  key: ShapeStyle
  roughness: number
  font: number
  strokeWidth: number
}

// Excalidraw's FONT_FAMILY ids: 5 is Excalifont, 6 is Nunito. Both ship with
// the canvas (served from /excalidraw-assets/), so neither depends on what
// the viewer has installed.
const SKETCH: Style = { key: "sketch", roughness: 1, font: 5, strokeWidth: 2 }
const CLEAN: Style = { key: "clean", roughness: 0, font: 6, strokeWidth: 1.5 }
/** Badges and chips are small and symbolic, so they are always drawn clean. */
const SYMBOL = CLEAN
const STYLES: Record<ShapeStyle, Style> = { sketch: SKETCH, clean: CLEAN }

// ---------------------------------------------------------------------------
// Colours: Excalidraw's own palette, so everything matches the colour picker.

const INK = "#1e1e1e"
const MUTED = "#868e96"
const WHITE = "#ffffff"

/** A role colours a card: a pale tile behind the icon, the icon itself, and
 * the card's outline. */
export interface Role {
  tile: string
  ink: string
  border: string
  /** Dark roles fill the whole card and use light text. */
  dark?: boolean
}

const ROLES = {
  infra: { tile: "#d0ebff", ink: "#1971c2", border: "#1971c2" },
  identity: { tile: "#e5dbff", ink: "#6741d9", border: "#6741d9" },
  data: { tile: "#c3fae8", ink: "#0c8599", border: "#0c8599" },
  network: { tile: "#d3f9d8", ink: "#2f9e44", border: "#2f9e44" },
  security: { tile: "#ffe3e3", ink: "#e03131", border: "#e03131" },
  offence: { tile: "#495057", ink: WHITE, border: "#212529", dark: true },
  loot: { tile: "#fff3bf", ink: "#f08c00", border: "#f08c00" },
  finding: { tile: "#ffe8cc", ink: "#e8590c", border: "#e8590c" },
} satisfies Record<string, Role>

/** Colours an agent can name with `color=`: Excalidraw's own palette (open
 * color), each as a pale tile and a strong ink, so a coloured card reads like
 * a built-in one. */
export const COLORS = {
  gray: { tile: "#e9ecef", ink: "#495057", border: "#495057" },
  red: { tile: "#ffe3e3", ink: "#e03131", border: "#e03131" },
  pink: { tile: "#ffdeeb", ink: "#c2255c", border: "#c2255c" },
  grape: { tile: "#f3d9fa", ink: "#9c36b5", border: "#9c36b5" },
  violet: { tile: "#e5dbff", ink: "#6741d9", border: "#6741d9" },
  indigo: { tile: "#dbe4ff", ink: "#3b5bdb", border: "#3b5bdb" },
  blue: { tile: "#d0ebff", ink: "#1971c2", border: "#1971c2" },
  cyan: { tile: "#c5f6fa", ink: "#0c8599", border: "#0c8599" },
  teal: { tile: "#c3fae8", ink: "#099268", border: "#099268" },
  green: { tile: "#d3f9d8", ink: "#2f9e44", border: "#2f9e44" },
  lime: { tile: "#e9fac8", ink: "#66a80f", border: "#66a80f" },
  yellow: { tile: "#fff3bf", ink: "#f08c00", border: "#f08c00" },
  orange: { tile: "#ffe8cc", ink: "#e8590c", border: "#e8590c" },
  black: { tile: "#495057", ink: WHITE, border: "#212529", dark: true },
} satisfies Record<string, Role>

export const COLOR_NAMES: readonly string[] = Object.keys(COLORS)

const HEX_RE = /^#[0-9a-f]{6}$/i

/** Whether a value names a colour: a palette name or #rrggbb. */
export function isColor(value: string): boolean {
  return value.toLowerCase() in COLORS || HEX_RE.test(value)
}

/** The look for a colour. A hex colour is the ink, with a pale tint of it as
 * the tile. */
export function colorRole(value: string): Role | null {
  const named = (COLORS as Record<string, Role>)[value.toLowerCase()]
  if (named) return named
  if (!HEX_RE.test(value)) return null
  const tint = (i: number) => {
    const c = parseInt(value.slice(1 + 2 * i, 3 + 2 * i), 16)
    return Math.round(c + (255 - c) * 0.82).toString(16).padStart(2, "0")
  }
  return { tile: `#${tint(0)}${tint(1)}${tint(2)}`, ink: value.toLowerCase(), border: value.toLowerCase() }
}

/** Every icon an agent can name with `icon=`. */
export const ICON_NAMES: readonly string[] = Object.keys(DRAWING_ICONS)

/** Words from this domain that lucide's keywords do not know, and the icons
 * that stand for them. Agents reach for these first ("icon=vault"). */
const ICON_SYNONYMS: Record<string, string[]> = {
  vault: ["lock-keyhole", "key-round", "shield"],
  hsm: ["key-round", "cpu", "lock-keyhole"],
  secret: ["key-round", "file-key", "lock"],
  secrets: ["key-round", "file-key", "lock"],
  kms: ["key-round", "lock-keyhole"],
  certificate: ["badge-check", "file-key"],
  pki: ["badge-check", "file-key", "key-round"],
  ca: ["badge-check", "file-key"],
  token: ["ticket", "key-round"],
  kerberos: ["ticket", "key-round"],
  mfa: ["smartphone", "badge-check", "key-square"],
  sso: ["key-round", "user-check"],
  idp: ["user-check", "id-card"],
  siem: ["chart-line", "activity", "eye"],
  soc: ["eye", "siren", "shield-check"],
  edr: ["shield-check", "scan-eye"],
  av: ["shield-check", "bug-off"],
  ids: ["radar", "scan-eye"],
  ips: ["shield-alert", "radar"],
  waf: ["brick-wall", "shield"],
  firewall: ["brick-wall", "brick-wall-fire", "shield"],
  fw: ["brick-wall", "shield"],
  proxy: ["arrow-right-left", "shield"],
  dns: ["globe", "at-sign"],
  cdn: ["globe", "cloud"],
  lb: ["split", "workflow"],
  loadbalancer: ["split", "workflow"],
  k8s: ["container", "boxes"],
  kubernetes: ["container", "boxes"],
  docker: ["container", "box"],
  bucket: ["archive", "database"],
  s3: ["archive", "database"],
  storage: ["hard-drive", "database", "archive"],
  backup: ["database-backup", "archive", "hard-drive-download"],
  log: ["file-text", "scroll"],
  logs: ["file-text", "scroll"],
  api: ["webhook", "code-xml", "braces"],
  c2: ["radio-tower", "satellite-dish"],
  beacon: ["radio-tower", "terminal"],
  implant: ["bug", "terminal"],
  malware: ["bug", "biohazard"],
  ransomware: ["file-lock", "lock", "biohazard"],
  phishing: ["fish", "mail-warning"],
  payload: ["package", "file-code"],
  exploit: ["zap", "bug"],
  vuln: ["triangle-alert", "bug"],
  iot: ["cpu", "radio"],
  ot: ["factory", "gauge"],
  scada: ["factory", "gauge"],
  plc: ["cpu", "factory"],
  dc: ["network", "server-cog"],
  ad: ["network", "users"],
  ldap: ["network", "users"],
  email: ["mail"],
  vpn: ["lock", "globe-lock"],
  tunnel: ["arrow-right-left", "route"],
  pivot: ["shuffle", "route"],
  password: ["key-round", "lock"],
  hash: ["hash", "binary"],
  database: ["database"],
  db: ["database"],
  sql: ["database"],
  person: ["user"],
  admin: ["user-cog", "crown"],
  root: ["crown", "user-cog"],
}

/** Icons that match a word by name or by meaning, best first: "vault" finds
 * lock and key by meaning, not by spelling. */
export function iconsFor(word: string, limit = 6): string[] {
  const w = word.toLowerCase()
  const scored: { name: string; score: number }[] = []
  for (const [rank, name] of (ICON_SYNONYMS[w] ?? []).entries()) {
    if (ICON_NAMES.includes(name)) scored.push({ name, score: 200 - rank })
  }
  for (const name of ICON_NAMES) {
    let score = 0
    if (name === w) score = 100
    else if (name.split("-").includes(w)) score = 60
    // Short names match inside almost any word ("x" in "zzqx"), so a part
    // has to be at least three letters to count.
    else if ((w.length >= 3 && name.includes(w)) || (name.length >= 3 && w.includes(name))) score = 40
    const tags = DRAWING_ICON_TAGS[name] ?? []
    if (tags.includes(w)) score = Math.max(score, 50)
    else if (tags.some((t) => t.includes(w) || (w.length > 3 && w.includes(t)))) score = Math.max(score, 20)
    if (score > 0 && !scored.some((s) => s.name === name)) scored.push({ name, score })
  }
  return scored.sort((a, b) => b.score - a.score || a.name.length - b.name.length).slice(0, limit).map((s) => s.name)
}

/** The usual severity scale. Medium is amber, which needs dark text. */
export const SEVERITIES = [
  { id: "critical", label: "CRITICAL", fill: "#c92a2a", text: WHITE },
  { id: "high", label: "HIGH", fill: "#e8590c", text: WHITE },
  { id: "medium", label: "MEDIUM", fill: "#fab005", text: INK },
  { id: "low", label: "LOW", fill: "#2f9e44", text: WHITE },
  { id: "info", label: "INFO", fill: "#1971c2", text: WHITE },
] as const

// ---------------------------------------------------------------------------
// Building blocks

const ROUND = { type: 3 } as const

/** A lucide icon as Excalidraw shapes, `size` pixels square at (x, y). */
export function icon(
  name: string,
  x: number,
  y: number,
  size: number,
  color: string,
  groupIds: string[],
): Skeleton[] {
  const shapes = DRAWING_ICONS[name]
  if (!shapes) throw new Error(`drawing icon "${name}" is not generated; add it to scripts/generate-drawing-icons.mjs`)
  const s = size / 24
  // lucide's 2px stroke, scaled with the icon.
  const strokeWidth = Math.max(1, Math.round(2 * s * 2) / 2)
  const common = { strokeColor: color, strokeWidth, roughness: 0, groupIds, customData: { part: "icon" } }
  return shapes.map((shape): Skeleton => {
    switch (shape.k) {
      case "e":
        return { type: "ellipse", x: x + shape.x * s, y: y + shape.y * s, width: shape.w * s, height: shape.h * s, backgroundColor: "transparent", ...common }
      case "r":
        return {
          type: "rectangle",
          x: x + shape.x * s,
          y: y + shape.y * s,
          width: shape.w * s,
          height: shape.h * s,
          backgroundColor: "transparent",
          roundness: shape.round ? ROUND : null,
          ...common,
        }
      case "d": {
        const d = strokeWidth * 1.2
        return {
          type: "ellipse",
          x: x + shape.x * s - d / 2,
          y: y + shape.y * s - d / 2,
          width: d,
          height: d,
          backgroundColor: color,
          fillStyle: "solid",
          ...common,
        }
      }
      case "l": {
        const [x0, y0] = shape.p[0]
        const points = shape.p.map(([px, py]) => [(px - x0) * s, (py - y0) * s] as [number, number])
        const xs = points.map((p) => p[0])
        const ys = points.map((p) => p[1])
        return {
          type: "line",
          x: x + x0 * s,
          y: y + y0 * s,
          width: Math.max(...xs) - Math.min(...xs),
          height: Math.max(...ys) - Math.min(...ys),
          points,
          ...common,
        }
      }
    }
  })
}

/** A rough width for text, to size a card around it. Generous: a card a
 * little wide reads fine, a title spilling out of it does not. */
function textWidth(text: string, fontSize: number): number {
  return text.length * fontSize * 0.6
}

function text(
  value: string,
  x: number,
  y: number,
  fontSize: number,
  color: string,
  style: Style,
  groupIds: string[],
): Skeleton {
  return { type: "text", x, y, text: value, fontSize, fontFamily: style.font, strokeColor: color, groupIds }
}

// ---------------------------------------------------------------------------
// Cards

const CARD_PAD = 14
const TILE = 48
const ICON = 28
export const CARD_HEIGHT = TILE + 2 * CARD_PAD
const BADGE = 34

interface CardSpec {
  /** The kind an agent writes ("domain-controller"), and the panel key. */
  kind: string
  name: string
  category: ShapeCategory
  role: keyof typeof ROLES
  icon: string
  title: string
  detail: string
  keywords: string[]
}

function lookOf(spec: CardSpec): { role: Role; icon: string } {
  return { role: ROLES[spec.role], icon: spec.icon }
}

/** The width a card needs for its words. */
export function cardWidth(title: string, detail: string): number {
  const textX = CARD_PAD + TILE + 12
  return Math.max(220, Math.ceil(textX + Math.max(textWidth(title, 20), textWidth(detail, 14)) + CARD_PAD))
}

/** The card: an outlined box, an icon on a pale tile, a title and a detail
 * line, and any badges on its top-right corner. */
function cardShapes(
  groupId: string,
  look: { role: Role; icon: string },
  title: string,
  detail: string,
  style: Style,
  badges: readonly string[] = [],
  opts: { border?: string; borderWidth?: number } = {},
): { elements: Skeleton[]; width: number } {
  const role = look.role
  const groupIds = [groupId]
  const textX = CARD_PAD + TILE + 12
  const width = cardWidth(title, detail)
  const titleColor = role.dark ? WHITE : INK
  const detailColor = role.dark ? "#ced4da" : MUTED
  const elements: Skeleton[] = [
    {
      type: "rectangle",
      x: 0,
      y: 0,
      width,
      height: CARD_HEIGHT,
      backgroundColor: role.dark ? "#343a40" : WHITE,
      fillStyle: "solid",
      strokeColor: opts.border ?? role.border,
      strokeWidth: opts.borderWidth ?? style.strokeWidth,
      roughness: style.roughness,
      roundness: ROUND,
      groupIds,
    },
    part(
      {
        type: "rectangle",
        x: CARD_PAD,
        y: CARD_PAD,
        width: TILE,
        height: TILE,
        backgroundColor: role.tile,
        fillStyle: "solid",
        strokeColor: "transparent",
        roughness: 0,
        roundness: ROUND,
        groupIds,
      },
      "tile",
    ),
    ...icon(look.icon, CARD_PAD + (TILE - ICON) / 2, CARD_PAD + (TILE - ICON) / 2, ICON, role.ink, groupIds),
    // Without a detail line the title sits in the middle of the card rather
    // than above an empty line.
    part(text(title, textX, detail ? CARD_PAD + 2 : CARD_PAD + 12, 20, titleColor, style, groupIds), "title"),
    ...(detail ? [part(text(detail, textX, CARD_PAD + 30, 14, detailColor, style, groupIds), "detail")] : []),
    ...badges.flatMap((key, i) => badgeOnCard(key, width, i, groupIds)),
  ]
  return { elements, width }
}

/** A badge on a card's top-right corner; a second one sits to its left. */
function badgeOnCard(key: string, cardW: number, index: number, groupIds: string[]): Skeleton[] {
  const spec = BADGES.find((b) => b.key === key)
  if (!spec) return []
  const x = cardW - BADGE / 2 - 4 - index * (BADGE + 4)
  return badgeShapes(spec.icon, spec.color, x, -BADGE / 2 + 2, groupIds).map((el) =>
    part(el, `badge:${key}`),
  )
}

/** A round status badge: a filled disc with a white icon. */
function badgeShapes(iconName: string, color: string, x: number, y: number, groupIds: string[]): Skeleton[] {
  return [
    {
      type: "ellipse",
      x,
      y,
      width: BADGE,
      height: BADGE,
      backgroundColor: color,
      fillStyle: "solid",
      strokeColor: WHITE,
      strokeWidth: 2,
      roughness: 0,
      groupIds,
    },
    ...icon(iconName, x + 8, y + 8, BADGE - 16, WHITE, groupIds),
  ]
}

function chipShapes(severity: (typeof SEVERITIES)[number], x: number, y: number, groupIds: string[]): Skeleton[] {
  return [
    {
      type: "rectangle",
      x,
      y,
      width: 104,
      height: 30,
      backgroundColor: severity.fill,
      fillStyle: "solid",
      strokeColor: severity.fill,
      strokeWidth: 1,
      roughness: 0,
      roundness: ROUND,
      groupIds,
      label: { text: severity.label, fontSize: 14, fontFamily: SYMBOL.font, strokeColor: severity.text, groupIds },
    },
  ]
}

// ---------------------------------------------------------------------------
// Connections and zones

/** How each kind of connection is drawn. The diagram language names them. */
export const EDGE_STYLES = {
  plain: { color: INK, dash: "solid", example: "" },
  traffic: { color: INK, dash: "solid", example: "HTTPS 443" },
  exploit: { color: "#e03131", dash: "solid", example: "exploit" },
  lateral: { color: "#e03131", dash: "dashed", example: "lateral movement" },
  creds: { color: "#f08c00", dash: "dashed", example: "credential reuse" },
  c2: { color: "#343a40", dash: "dotted", example: "C2 channel" },
} as const

export type EdgeStyle = keyof typeof EDGE_STYLES

function arrowShapes(label: string, edge: EdgeStyle, style: Style): Skeleton[] {
  const { color, dash } = EDGE_STYLES[edge]
  return [
    {
      type: "arrow",
      x: 0,
      y: 0,
      width: 240,
      height: 0,
      strokeColor: color,
      strokeStyle: dash,
      strokeWidth: 2,
      roughness: style.roughness,
      ...(label ? { label: { text: label, fontSize: 16, fontFamily: style.font, strokeColor: color } } : {}),
    },
  ]
}

/** The kinds of region, and how each is drawn. */
export const ZONE_KINDS = {
  network: { icon: "network", color: "#1971c2", example: "Internal — 10.0.0.0/24" },
  dmz: { icon: "globe", color: "#e8590c", example: "DMZ — 172.16.0.0/24" },
  cloud: { icon: "cloud", color: "#0c8599", example: "Cloud tenant" },
  trust: { icon: "shield-check", color: "#e03131", example: "Trust boundary" },
} as const

export type ZoneKind = keyof typeof ZONE_KINDS

/** Room a zone leaves above its contents for its title. */
export const ZONE_TITLE_SPACE = 44

/** A dashed region with an icon and a title in its corner: a subnet, a DMZ,
 * a cloud tenant, a trust boundary. */
function zoneShapes(
  groupId: string,
  kind: ZoneKind,
  title: string,
  style: Style,
  width = 460,
  height = 280,
): Skeleton[] {
  const { icon: iconName, color } = ZONE_KINDS[kind]
  const groupIds = [groupId]
  return [
    {
      type: "rectangle",
      x: 0,
      y: 0,
      width,
      height,
      backgroundColor: "transparent",
      strokeColor: color,
      strokeStyle: "dashed",
      strokeWidth: 2,
      roughness: style.roughness,
      roundness: ROUND,
      groupIds,
    },
    ...icon(iconName, 14, 12, 20, color, groupIds),
    part(text(title, 42, 12, 16, color, style, groupIds), "title"),
  ]
}

// ---------------------------------------------------------------------------
// The catalogue

export const SHAPE_CATEGORIES = [
  { id: "hosts", label: "Hosts" },
  { id: "identity", label: "Identity" },
  { id: "services", label: "Data & services" },
  { id: "network", label: "Network" },
  { id: "defences", label: "Defences" },
  { id: "offence", label: "Offence" },
  { id: "loot", label: "Loot & findings" },
  { id: "status", label: "Status badges" },
  { id: "severity", label: "Severity" },
  { id: "connections", label: "Connections" },
  { id: "zones", label: "Zones" },
  { id: "notes", label: "Notes & legends" },
] as const

export type ShapeCategory = (typeof SHAPE_CATEGORIES)[number]["id"]

const CARDS: CardSpec[] = [
  { kind: "server", name: "Server", category: "hosts", role: "infra", icon: "server", title: "Server", detail: "10.0.5.20 · Ubuntu 22.04", keywords: ["host", "linux", "machine"] },
  { kind: "workstation", name: "Workstation", category: "hosts", role: "infra", icon: "monitor", title: "Workstation", detail: "WS-0142 · Windows 11", keywords: ["desktop", "pc", "endpoint", "host"] },
  { kind: "laptop", name: "Laptop", category: "hosts", role: "infra", icon: "laptop", title: "Laptop", detail: "LT-0087 · macOS", keywords: ["endpoint", "host", "notebook"] },
  { kind: "mobile", name: "Mobile device", category: "hosts", role: "infra", icon: "smartphone", title: "Mobile", detail: "Android 14", keywords: ["phone", "ios", "android"] },
  { kind: "ot-device", name: "OT / IoT device", category: "hosts", role: "infra", icon: "cpu", title: "PLC", detail: "OT segment · Modbus", keywords: ["scada", "ics", "plc", "iot", "embedded"] },
  { kind: "file-share", name: "File share", category: "hosts", role: "infra", icon: "folder-open", title: "File share", detail: "\\\\fs01\\finance", keywords: ["smb", "nas", "share", "cifs"] },
  { kind: "domain-controller", name: "Domain controller", category: "identity", role: "identity", icon: "network", title: "DC01", detail: "Domain Controller · corp.local", keywords: ["dc", "active directory", "ad", "kerberos", "ldap"] },
  { kind: "user", name: "User", category: "identity", role: "identity", icon: "user", title: "j.smith", detail: "Domain user", keywords: ["account", "person", "employee", "victim"] },
  { kind: "admin", name: "Administrator", category: "identity", role: "identity", icon: "user-cog", title: "adm.jsmith", detail: "Domain admin", keywords: ["da", "privileged", "account", "root"] },
  { kind: "database", name: "Database", category: "services", role: "data", icon: "database", title: "Database", detail: "MSSQL · 1433", keywords: ["sql", "db", "mysql", "postgres", "oracle"] },
  { kind: "web-app", name: "Web application", category: "services", role: "data", icon: "app-window", title: "Web app", detail: "https://portal.corp.local", keywords: ["website", "http", "portal", "api"] },
  { kind: "mail-server", name: "Mail server", category: "services", role: "data", icon: "mail", title: "Mail", detail: "Exchange 2019", keywords: ["exchange", "smtp", "email", "phishing"] },
  { kind: "internet", name: "Internet", category: "network", role: "network", icon: "globe", title: "Internet", detail: "Public network", keywords: ["wan", "external", "public"] },
  { kind: "cloud", name: "Cloud service", category: "network", role: "network", icon: "cloud", title: "Cloud", detail: "Azure tenant", keywords: ["aws", "azure", "gcp", "saas", "m365"] },
  { kind: "router", name: "Router", category: "network", role: "network", icon: "router", title: "Router", detail: "10.0.0.1", keywords: ["gateway", "network"] },
  { kind: "switch", name: "Switch", category: "network", role: "network", icon: "ethernet-port", title: "Switch", detail: "Core · VLAN 10, 20", keywords: ["vlan", "network", "lan"] },
  { kind: "vpn", name: "VPN gateway", category: "network", role: "network", icon: "lock", title: "VPN", detail: "vpn.corp.com", keywords: ["remote access", "tunnel", "ipsec", "wireguard"] },
  { kind: "firewall", name: "Firewall", category: "defences", role: "security", icon: "brick-wall", title: "Firewall", detail: "Perimeter", keywords: ["fw", "waf", "filter", "acl"] },
  { kind: "edr", name: "EDR / security control", category: "defences", role: "security", icon: "shield-check", title: "EDR", detail: "Defender for Endpoint", keywords: ["av", "antivirus", "xdr", "siem", "soc", "defence"] },
  { kind: "attacker", name: "Attacker", category: "offence", role: "offence", icon: "venetian-mask", title: "Attacker", detail: "Operator workstation", keywords: ["red team", "adversary", "operator", "hacker"] },
  { kind: "c2-server", name: "C2 server", category: "offence", role: "offence", icon: "radio-tower", title: "C2 server", detail: "team server · 443", keywords: ["command and control", "cobalt strike", "sliver", "teamserver", "redirector"] },
  { kind: "shell", name: "Shell / implant", category: "offence", role: "offence", icon: "terminal", title: "Beacon", detail: "SYSTEM · pid 4120", keywords: ["beacon", "agent", "implant", "session", "reverse shell"] },
  { kind: "credential", name: "Credential", category: "loot", role: "loot", icon: "key-round", title: "Credential", detail: "svc_backup · cleartext", keywords: ["password", "secret", "login", "creds"] },
  { kind: "hash", name: "Hash", category: "loot", role: "loot", icon: "hash", title: "NTLM hash", detail: "Administrator", keywords: ["ntlm", "kerberos", "tgt", "crack"] },
  { kind: "vulnerability", name: "Vulnerability", category: "loot", role: "finding", icon: "triangle-alert", title: "Vulnerability", detail: "CVE-2024-XXXX", keywords: ["cve", "exploit", "weakness", "vuln"] },
  { kind: "finding", name: "Finding", category: "loot", role: "finding", icon: "bug", title: "Finding", detail: "Weak password policy", keywords: ["issue", "report", "misconfiguration"] },
]

/** Every card kind, in catalogue order. */
export const CARD_KINDS: readonly string[] = CARDS.map((c) => c.kind)

export const BADGES = [
  { key: "compromised", name: "Compromised", icon: "skull", color: "#e03131", keywords: ["pwned", "owned", "hacked"] },
  { key: "credentials", name: "Credentials found", icon: "key-round", color: "#f08c00", keywords: ["password", "creds", "loot"] },
  { key: "privileged", name: "Privileged access", icon: "crown", color: "#6741d9", keywords: ["admin", "root", "system", "da"] },
  { key: "flag", name: "Objective reached", icon: "flag", color: "#2f9e44", keywords: ["goal", "crown jewel", "captured"] },
  { key: "has-finding", name: "Has a finding", icon: "bug", color: "#e8590c", keywords: ["vulnerability", "issue"] },
  { key: "monitored", name: "Monitored / EDR", icon: "eye", color: "#1971c2", keywords: ["detection", "edr", "logging", "soc"] },
  { key: "out-of-scope", name: "Out of scope", icon: "ban", color: "#868e96", keywords: ["excluded", "do not touch"] },
  { key: "target", name: "Target", icon: "crosshair", color: "#c92a2a", keywords: ["objective", "goal"] },
] as const

export const BADGE_KEYS: readonly string[] = BADGES.map((b) => b.key)

// ---------------------------------------------------------------------------
// Builders for the diagram language

export interface CardRequest {
  /** A card kind, or "box" for a card of the agent's own: then `icon` is
   * required and `color` picks its look. */
  kind: string
  title: string
  detail: string
  style: ShapeStyle
  badges?: readonly string[]
  node?: string
  /** A palette name or #rrggbb, overriding the kind's own colours. */
  color?: string
  /** A lucide icon name (ICON_NAMES), overriding the kind's own icon. */
  icon?: string
}

/** A card as the diagram language asks for it, at the origin. Throws on an
 * unknown kind, colour or icon; the language checks them before here. */
export function buildCard(req: CardRequest): { elements: Skeleton[]; width: number; height: number } {
  const spec = CARDS.find((c) => c.kind === req.kind)
  if (!spec && req.kind !== "box") throw new Error(`unknown card kind "${req.kind}"`)
  const iconName = req.icon ?? spec?.icon
  if (!iconName) throw new Error("a card of your own needs an icon")
  const role = req.color ? colorRole(req.color) : spec ? ROLES[spec.role] : COLORS.gray
  if (!role) throw new Error(`unknown colour "${req.color}"`)
  const style = STYLES[req.style]
  const card = cardShapes(`card-${req.kind}`, { role, icon: iconName }, req.title, req.detail, style, req.badges ?? [])
  return {
    elements: tagged(card.elements, {
      shape: `card:${req.kind}`,
      style: req.style,
      ...(req.node ? { node: req.node } : {}),
      ...(req.color ? { color: req.color } : {}),
      ...(req.icon ? { icon: req.icon } : {}),
    }),
    width: card.width,
    height: CARD_HEIGHT,
  }
}

/** The look of a plain shape in a chosen colour: its outline in the ink and
 * a pale fill. */
export function plainShapeColors(
  color: string,
): { strokeColor: string; backgroundColor: string; textColor: string } | null {
  const role = colorRole(color)
  if (!role) return null
  return role.dark
    ? { strokeColor: role.border, backgroundColor: "#343a40", textColor: WHITE }
    : { strokeColor: role.border, backgroundColor: role.tile, textColor: INK }
}

/** One badge, tagged as a part of the card it sits on. `cardWidth` and
 * `index` place it on the card's corner, the way buildCard would. */
export function buildBadgeOnCard(
  key: string,
  cardW: number,
  index: number,
  groupIds: string[],
  tag: Omit<ShapeTag, "part">,
): Skeleton[] {
  return tagged(badgeOnCard(key, cardW, index, groupIds), tag)
}

export interface ZoneRequest {
  kind: ZoneKind
  title: string
  width: number
  height: number
  style: ShapeStyle
  node?: string
}

export function buildZone(req: ZoneRequest): Skeleton[] {
  return tagged(
    zoneShapes(`zone-${req.kind}`, req.kind, req.title, STYLES[req.style], req.width, req.height),
    { shape: `zone:${req.kind}`, style: req.style, ...(req.node ? { node: req.node } : {}) },
  )
}

/** The look of a connection, without geometry: the diagram language binds it
 * to its two ends and lets the server draw the line. */
export function edgeLook(edge: EdgeStyle, style: ShapeStyle) {
  const { color, dash } = EDGE_STYLES[edge]
  return {
    strokeColor: color,
    strokeStyle: dash,
    strokeWidth: 2,
    roughness: STYLES[style].roughness,
    fontFamily: STYLES[style].font,
    customData: { logos: { shape: `edge:${edge}`, part: "line", style } satisfies ShapeTag },
  }
}

// ---------------------------------------------------------------------------
// The panel's catalogue

export interface BuiltinShape {
  /** Stable and unique; the panel caches by it. */
  key: string
  name: string
  category: ShapeCategory
  /** Extra words search matches, besides the name. */
  keywords: string[]
  /** Whether the two styles differ. Badges and chips are always clean. */
  styled: boolean
  /** How much room the panel gives it: a card per half row, or a badge per
   * quarter. */
  tile: "wide" | "small"
  build(style: ShapeStyle): Skeleton[]
}

/** The showcase: a card already marked compromised, badge on its corner. */
function compromisedHost(style: Style): Skeleton[] {
  const spec = CARDS.find((c) => c.kind === "server") as CardSpec
  const card = cardShapes("card-compromised", lookOf(spec), "SRV-APP01", "10.0.5.31 · pwned via CVE", style, ["compromised"], {
    border: "#e03131",
    borderWidth: 3,
  })
  return tagged(card.elements, { shape: "card:server", style: style.key })
}

function severityLegend(): Skeleton[] {
  const groupIds = ["legend-severity"]
  return tagged(
    [
      text("Severity", 0, 0, 18, INK, SYMBOL, groupIds),
      ...SEVERITIES.flatMap((sev, i) => chipShapes(sev, 0, 30 + i * 38, groupIds)),
    ],
    { shape: "legend:severity" },
  )
}

function statusLegend(): Skeleton[] {
  const groupIds = ["legend-status"]
  return tagged(
    [
      text("Status", 0, 0, 18, INK, SYMBOL, groupIds),
      ...BADGES.flatMap((badge, i) => [
        ...badgeShapes(badge.icon, badge.color, 0, 30 + i * 42, groupIds),
        text(badge.name, BADGE + 10, 30 + i * 42 + 8, 16, INK, SYMBOL, groupIds),
      ]),
    ],
    { shape: "legend:status" },
  )
}

function note(style: Style): Skeleton[] {
  return tagged(
    [
      {
        type: "rectangle",
        x: 0,
        y: 0,
        width: 220,
        height: 130,
        backgroundColor: "#fff3bf",
        fillStyle: "solid",
        strokeColor: "#f08c00",
        strokeWidth: 1,
        roughness: style.roughness,
        label: { text: "Note", fontSize: 16, fontFamily: style.font, textAlign: "left", verticalAlign: "top" },
      },
    ],
    { shape: "note", style: style.key },
  )
}

function step(): Skeleton[] {
  return tagged(
    [
      {
        type: "ellipse",
        x: 0,
        y: 0,
        width: 40,
        height: 40,
        backgroundColor: "#e03131",
        fillStyle: "solid",
        strokeColor: WHITE,
        strokeWidth: 2,
        roughness: 0,
        label: { text: "1", fontSize: 20, fontFamily: SYMBOL.font, strokeColor: WHITE },
      },
    ],
    { shape: "step" },
  )
}

type Entry = Omit<BuiltinShape, "build"> & { build: (style: Style) => Skeleton[] }

const EDGE_ENTRIES: { edge: EdgeStyle; name: string; keywords: string[] }[] = [
  { edge: "traffic", name: "Traffic", keywords: ["arrow", "flow", "connection", "port"] },
  { edge: "exploit", name: "Exploit", keywords: ["arrow", "attack", "rce"] },
  { edge: "lateral", name: "Lateral movement", keywords: ["arrow", "pivot", "psexec", "wmi", "rdp"] },
  { edge: "creds", name: "Credential reuse", keywords: ["arrow", "password spray", "pass the hash"] },
  { edge: "c2", name: "C2 channel", keywords: ["arrow", "beacon", "callback", "command and control"] },
]

const ZONE_ENTRIES: { kind: ZoneKind; name: string; keywords: string[] }[] = [
  { kind: "network", name: "Network zone", keywords: ["subnet", "segment", "vlan", "lan", "internal"] },
  { kind: "dmz", name: "DMZ", keywords: ["perimeter", "subnet", "segment"] },
  { kind: "cloud", name: "Cloud tenant", keywords: ["aws", "azure", "gcp", "vpc", "subscription"] },
  { kind: "trust", name: "Trust boundary", keywords: ["boundary", "scope", "forest", "domain"] },
]

const ENTRIES: Entry[] = [
  ...CARDS.map((spec): Entry => ({
    key: `card-${spec.kind}`,
    name: spec.name,
    category: spec.category,
    keywords: [...spec.keywords, spec.title],
    styled: true,
    tile: "wide",
    build: (style) =>
      tagged(cardShapes(`card-${spec.kind}`, lookOf(spec), spec.title, spec.detail, style).elements, {
        shape: `card:${spec.kind}`,
        style: style.key,
      }),
  })),
  {
    key: "card-compromised-host",
    name: "Compromised host",
    category: "offence",
    keywords: ["pwned", "owned", "hacked", "foothold"],
    styled: true,
    tile: "wide",
    build: compromisedHost,
  },
  ...BADGES.map((badge): Entry => ({
    key: `badge-${badge.key}`,
    name: badge.name,
    category: "status",
    keywords: ["badge", "status", ...badge.keywords],
    styled: false,
    tile: "small",
    build: () => tagged(badgeShapes(badge.icon, badge.color, 0, 0, [`badge-${badge.key}`]), { shape: `badge:${badge.key}` }),
  })),
  ...SEVERITIES.map((sev): Entry => ({
    key: `severity-${sev.id}`,
    name: sev.label.charAt(0) + sev.label.slice(1).toLowerCase(),
    category: "severity",
    keywords: ["severity", "risk", "cvss", sev.id],
    styled: false,
    tile: "small",
    build: () => tagged(chipShapes(sev, 0, 0, []), { shape: `severity:${sev.id}` }),
  })),
  ...EDGE_ENTRIES.map((e): Entry => ({
    key: `arrow-${e.edge}`,
    name: e.name,
    category: "connections",
    keywords: e.keywords,
    styled: true,
    tile: "wide",
    build: (style) =>
      tagged(arrowShapes(EDGE_STYLES[e.edge].example, e.edge, style), { shape: `edge:${e.edge}`, style: style.key }, ["line"]),
  })),
  ...ZONE_ENTRIES.map((z): Entry => ({
    key: `zone-${z.kind}`,
    name: z.name,
    category: "zones",
    keywords: z.keywords,
    styled: true,
    tile: "wide",
    build: (style) =>
      tagged(zoneShapes(`zone-${z.kind}`, z.kind, ZONE_KINDS[z.kind].example, style), { shape: `zone:${z.kind}`, style: style.key }),
  })),
  { key: "step", name: "Numbered step", category: "notes", keywords: ["number", "order", "sequence", "marker"], styled: false, tile: "wide", build: step },
  { key: "note", name: "Note", category: "notes", keywords: ["sticky", "comment", "text"], styled: true, tile: "wide", build: note },
  { key: "legend-severity", name: "Severity legend", category: "notes", keywords: ["legend", "key", "severity"], styled: false, tile: "wide", build: severityLegend },
  { key: "legend-status", name: "Status legend", category: "notes", keywords: ["legend", "key", "badges", "status"], styled: false, tile: "wide", build: statusLegend },
]

export const BUILTIN_SHAPES: readonly BuiltinShape[] = ENTRIES.map((entry) => ({
  ...entry,
  build: (style: ShapeStyle) => entry.build(STYLES[style]),
}))

/** Whether a shape matches a search: every word of the query must appear in
 * its name, its keywords or its category. */
export function matchesShape(shape: BuiltinShape, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const category = SHAPE_CATEGORIES.find((c) => c.id === shape.category)?.label ?? ""
  const haystack = [shape.name, category, ...shape.keywords].join(" ").toLowerCase()
  return words.every((word) => haystack.includes(word))
}

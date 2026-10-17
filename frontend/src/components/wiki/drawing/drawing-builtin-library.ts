// The built-in shapes every drawing offers, in the Shapes panel
// (drawing-shapes-panel.tsx).
//
// Drawings here are network maps, attack paths and infrastructure sketches
// (see core/pkg/mcp/skillassets/drawings.md). These shapes give them one
// visual language, so a map one operator drew reads the same to the next:
//
//   - cards: an icon tile, a title and a detail line ("DC01", "10.0.5.10 ·
//     Windows 2019"), coloured by role: infrastructure, identity, network,
//     offence, loot, finding;
//   - status badges that sit on a card's corner: compromised, credentials,
//     privileged, flag, finding, monitored, out of scope, target;
//   - severity chips on the usual critical-to-info scale, and legends for
//     both;
//   - connections, zones, a step marker and a note.
//
// Most shapes come in two styles, picked with a switch in the panel. Sketch
// is Excalidraw's hand-drawn look, for working diagrams. Clean has straight
// lines and Nunito, for diagrams that go into a client report.
//
// Icons are lucide's (the set the app uses everywhere else), converted into
// Excalidraw's own lines and ellipses by scripts/generate-drawing-icons.mjs.
// They are always drawn with straight lines, even in the sketch style: a
// 28px icon drawn rough turns into scribble.
//
// Everything is an Excalidraw *skeleton*, the library's documented input for
// building elements (convertToExcalidrawElements). The panel converts them,
// so this module imports only types and stays testable without a browser.
// The parts of a shape share a group id, so it inserts and moves as one.
// Every inserted copy gets fresh ids and groups (drawing-insert.ts).

import type { ExcalidrawElementSkeleton } from "@excalidraw/excalidraw/data/transform"
import type { FontFamilyValues } from "@excalidraw/excalidraw/element/types"

import { DRAWING_ICONS } from "./drawing-icons.generated"

export type ShapeStyle = "sketch" | "clean"

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
  build(style: ShapeStyle): ExcalidrawElementSkeleton[]
}

type Skeleton = ExcalidrawElementSkeleton

// ---------------------------------------------------------------------------
// Styles

interface Style {
  key: ShapeStyle
  roughness: number
  font: FontFamilyValues
  strokeWidth: number
}

// Excalidraw's FONT_FAMILY ids: 5 is Excalifont, 6 is Nunito. Both ship with
// the canvas (served from /excalidraw-assets/), so neither depends on what
// the viewer has installed.
const SKETCH: Style = { key: "sketch", roughness: 1, font: 5 as FontFamilyValues, strokeWidth: 2 }
const CLEAN: Style = { key: "clean", roughness: 0, font: 6 as FontFamilyValues, strokeWidth: 1.5 }
/** Badges and chips are small and symbolic, so they are always drawn clean. */
const SYMBOL = CLEAN

// ---------------------------------------------------------------------------
// Colours: Excalidraw's own palette, so everything matches the colour picker.

const INK = "#1e1e1e"
const MUTED = "#868e96"
const WHITE = "#ffffff"

/** A role colours a card: a pale tile behind the icon, the icon itself, and
 * the card's outline. */
interface Role {
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
  const common = { strokeColor: color, strokeWidth, roughness: 0, groupIds }
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
        } as Skeleton
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

const CARD_PAD = 14
const TILE = 48
const ICON = 28
const CARD_HEIGHT = TILE + 2 * CARD_PAD

/** The card: an outlined box, an icon on a pale tile, a title and a detail
 * line. Returns the box's width too, so a badge can be placed on its
 * corner. */
function cardShapes(
  groupId: string,
  roleKey: keyof typeof ROLES,
  iconName: string,
  title: string,
  detail: string,
  style: Style,
  opts: { border?: string; borderWidth?: number } = {},
): { elements: Skeleton[]; width: number } {
  const role: Role = ROLES[roleKey]
  const groupIds = [groupId]
  const textX = CARD_PAD + TILE + 12
  const width = Math.max(
    220,
    Math.ceil(textX + Math.max(textWidth(title, 20), textWidth(detail, 14)) + CARD_PAD),
  )
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
    ...icon(iconName, CARD_PAD + (TILE - ICON) / 2, CARD_PAD + (TILE - ICON) / 2, ICON, role.ink, groupIds),
    text(title, textX, CARD_PAD + 2, 20, titleColor, style, groupIds),
    text(detail, textX, CARD_PAD + 30, 14, detailColor, style, groupIds),
  ]
  return { elements, width }
}

const BADGE = 34

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

function arrowShapes(label: string, color: string, dash: "solid" | "dashed" | "dotted", style: Style): Skeleton[] {
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
      label: { text: label, fontSize: 16, fontFamily: style.font, strokeColor: color },
    },
  ]
}

/** A dashed region with an icon and a title in its corner: a subnet, a DMZ,
 * a cloud tenant, a trust boundary. */
function zoneShapes(groupId: string, iconName: string, title: string, color: string, style: Style): Skeleton[] {
  const groupIds = [groupId]
  return [
    {
      type: "rectangle",
      x: 0,
      y: 0,
      width: 460,
      height: 280,
      backgroundColor: "transparent",
      strokeColor: color,
      strokeStyle: "dashed",
      strokeWidth: 2,
      roughness: style.roughness,
      roundness: ROUND,
      groupIds,
    },
    ...icon(iconName, 14, 12, 20, color, groupIds),
    text(title, 42, 12, 16, color, style, groupIds),
  ]
}

// ---------------------------------------------------------------------------
// The catalogue

const STYLES: Record<ShapeStyle, Style> = { sketch: SKETCH, clean: CLEAN }

interface CardSpec {
  key: string
  name: string
  category: ShapeCategory
  role: keyof typeof ROLES
  icon: string
  title: string
  detail: string
  keywords: string[]
}

const CARDS: CardSpec[] = [
  { key: "server", name: "Server", category: "hosts", role: "infra", icon: "server", title: "Server", detail: "10.0.5.20 · Ubuntu 22.04", keywords: ["host", "linux", "machine"] },
  { key: "workstation", name: "Workstation", category: "hosts", role: "infra", icon: "monitor", title: "Workstation", detail: "WS-0142 · Windows 11", keywords: ["desktop", "pc", "endpoint", "host"] },
  { key: "laptop", name: "Laptop", category: "hosts", role: "infra", icon: "laptop", title: "Laptop", detail: "LT-0087 · macOS", keywords: ["endpoint", "host", "notebook"] },
  { key: "mobile", name: "Mobile device", category: "hosts", role: "infra", icon: "smartphone", title: "Mobile", detail: "Android 14", keywords: ["phone", "ios", "android"] },
  { key: "ot-device", name: "OT / IoT device", category: "hosts", role: "infra", icon: "cpu", title: "PLC", detail: "OT segment · Modbus", keywords: ["scada", "ics", "plc", "iot", "embedded"] },
  { key: "file-share", name: "File share", category: "hosts", role: "infra", icon: "folder-open", title: "File share", detail: "\\\\fs01\\finance", keywords: ["smb", "nas", "share", "cifs"] },
  { key: "domain-controller", name: "Domain controller", category: "identity", role: "identity", icon: "network", title: "DC01", detail: "Domain Controller · corp.local", keywords: ["dc", "active directory", "ad", "kerberos", "ldap"] },
  { key: "user", name: "User", category: "identity", role: "identity", icon: "user", title: "j.smith", detail: "Domain user", keywords: ["account", "person", "employee", "victim"] },
  { key: "admin", name: "Administrator", category: "identity", role: "identity", icon: "user-cog", title: "adm.jsmith", detail: "Domain admin", keywords: ["da", "privileged", "account", "root"] },
  { key: "database", name: "Database", category: "services", role: "data", icon: "database", title: "Database", detail: "MSSQL · 1433", keywords: ["sql", "db", "mysql", "postgres", "oracle"] },
  { key: "web-app", name: "Web application", category: "services", role: "data", icon: "app-window", title: "Web app", detail: "https://portal.corp.local", keywords: ["website", "http", "portal", "api"] },
  { key: "mail-server", name: "Mail server", category: "services", role: "data", icon: "mail", title: "Mail", detail: "Exchange 2019", keywords: ["exchange", "smtp", "email", "phishing"] },
  { key: "internet", name: "Internet", category: "network", role: "network", icon: "globe", title: "Internet", detail: "Public network", keywords: ["wan", "external", "public"] },
  { key: "cloud", name: "Cloud service", category: "network", role: "network", icon: "cloud", title: "Cloud", detail: "Azure tenant", keywords: ["aws", "azure", "gcp", "saas", "m365"] },
  { key: "router", name: "Router", category: "network", role: "network", icon: "router", title: "Router", detail: "10.0.0.1", keywords: ["gateway", "network"] },
  { key: "switch", name: "Switch", category: "network", role: "network", icon: "ethernet-port", title: "Switch", detail: "Core · VLAN 10, 20", keywords: ["vlan", "network", "lan"] },
  { key: "vpn", name: "VPN gateway", category: "network", role: "network", icon: "lock", title: "VPN", detail: "vpn.corp.com", keywords: ["remote access", "tunnel", "ipsec", "wireguard"] },
  { key: "firewall", name: "Firewall", category: "defences", role: "security", icon: "brick-wall", title: "Firewall", detail: "Perimeter", keywords: ["fw", "waf", "filter", "acl"] },
  { key: "edr", name: "EDR / security control", category: "defences", role: "security", icon: "shield-check", title: "EDR", detail: "Defender for Endpoint", keywords: ["av", "antivirus", "xdr", "siem", "soc", "defence"] },
  { key: "attacker", name: "Attacker", category: "offence", role: "offence", icon: "venetian-mask", title: "Attacker", detail: "Operator workstation", keywords: ["red team", "adversary", "operator", "hacker"] },
  { key: "c2-server", name: "C2 server", category: "offence", role: "offence", icon: "radio-tower", title: "C2 server", detail: "team server · 443", keywords: ["command and control", "cobalt strike", "sliver", "teamserver", "redirector"] },
  { key: "shell", name: "Shell / implant", category: "offence", role: "offence", icon: "terminal", title: "Beacon", detail: "SYSTEM · pid 4120", keywords: ["beacon", "agent", "implant", "session", "reverse shell"] },
  { key: "credential", name: "Credential", category: "loot", role: "loot", icon: "key-round", title: "Credential", detail: "svc_backup · cleartext", keywords: ["password", "secret", "login", "creds"] },
  { key: "hash", name: "Hash", category: "loot", role: "loot", icon: "hash", title: "NTLM hash", detail: "Administrator", keywords: ["ntlm", "kerberos", "tgt", "crack"] },
  { key: "vulnerability", name: "Vulnerability", category: "loot", role: "finding", icon: "triangle-alert", title: "Vulnerability", detail: "CVE-2024-XXXX", keywords: ["cve", "exploit", "weakness", "vuln"] },
  { key: "finding", name: "Finding", category: "loot", role: "finding", icon: "bug", title: "Finding", detail: "Weak password policy", keywords: ["issue", "report", "misconfiguration"] },
]

const BADGES = [
  { key: "compromised", name: "Compromised", icon: "skull", color: "#e03131", keywords: ["pwned", "owned", "hacked"] },
  { key: "credentials", name: "Credentials found", icon: "key-round", color: "#f08c00", keywords: ["password", "creds", "loot"] },
  { key: "privileged", name: "Privileged access", icon: "crown", color: "#6741d9", keywords: ["admin", "root", "system", "da"] },
  { key: "flag", name: "Objective reached", icon: "flag", color: "#2f9e44", keywords: ["goal", "crown jewel", "captured"] },
  { key: "has-finding", name: "Has a finding", icon: "bug", color: "#e8590c", keywords: ["vulnerability", "issue"] },
  { key: "monitored", name: "Monitored / EDR", icon: "eye", color: "#1971c2", keywords: ["detection", "edr", "logging", "soc"] },
  { key: "out-of-scope", name: "Out of scope", icon: "ban", color: "#868e96", keywords: ["excluded", "do not touch"] },
  { key: "target", name: "Target", icon: "crosshair", color: "#c92a2a", keywords: ["objective", "goal"] },
] as const

/** The showcase: a card already marked compromised, badge on its corner. */
function compromisedHost(style: Style): Skeleton[] {
  const groupId = "card-compromised"
  const card = cardShapes(groupId, "infra", "server", "SRV-APP01", "10.0.5.31 · pwned via CVE", style, {
    border: "#e03131",
    borderWidth: 3,
  })
  return [...card.elements, ...badgeShapes("skull", "#e03131", card.width - BADGE / 2 - 4, -BADGE / 2 + 2, [groupId])]
}

function severityLegend(): Skeleton[] {
  const groupIds = ["legend-severity"]
  return [
    text("Severity", 0, 0, 18, INK, SYMBOL, groupIds),
    ...SEVERITIES.flatMap((sev, i) => chipShapes(sev, 0, 30 + i * 38, groupIds)),
  ]
}

function statusLegend(): Skeleton[] {
  const groupIds = ["legend-status"]
  return [
    text("Status", 0, 0, 18, INK, SYMBOL, groupIds),
    ...BADGES.flatMap((badge, i) => [
      ...badgeShapes(badge.icon, badge.color, 0, 30 + i * 42, groupIds),
      text(badge.name, BADGE + 10, 30 + i * 42 + 8, 16, INK, SYMBOL, groupIds),
    ]),
  ]
}

function note(style: Style): Skeleton[] {
  return [
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
  ]
}

function step(): Skeleton[] {
  return [
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
  ]
}

type Entry = Omit<BuiltinShape, "build"> & { build: (style: Style) => Skeleton[] }

const ENTRIES: Entry[] = [
  ...CARDS.map((spec): Entry => ({
    key: `card-${spec.key}`,
    name: spec.name,
    category: spec.category,
    keywords: [...spec.keywords, spec.title],
    styled: true,
    tile: "wide",
    build: (style) => cardShapes(`card-${spec.key}`, spec.role, spec.icon, spec.title, spec.detail, style).elements,
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
    build: () => badgeShapes(badge.icon, badge.color, 0, 0, [`badge-${badge.key}`]),
  })),
  ...SEVERITIES.map((sev): Entry => ({
    key: `severity-${sev.id}`,
    name: sev.label.charAt(0) + sev.label.slice(1).toLowerCase(),
    category: "severity",
    keywords: ["severity", "risk", "cvss", sev.id],
    styled: false,
    tile: "small",
    build: () => chipShapes(sev, 0, 0, []),
  })),
  { key: "arrow-traffic", name: "Traffic", category: "connections", keywords: ["arrow", "flow", "connection", "port"], styled: true, tile: "wide", build: (style) => arrowShapes("HTTPS 443", INK, "solid", style) },
  { key: "arrow-exploit", name: "Exploit", category: "connections", keywords: ["arrow", "attack", "rce"], styled: true, tile: "wide", build: (style) => arrowShapes("exploit", "#e03131", "solid", style) },
  { key: "arrow-lateral", name: "Lateral movement", category: "connections", keywords: ["arrow", "pivot", "psexec", "wmi", "rdp"], styled: true, tile: "wide", build: (style) => arrowShapes("lateral movement", "#e03131", "dashed", style) },
  { key: "arrow-creds", name: "Credential reuse", category: "connections", keywords: ["arrow", "password spray", "pass the hash"], styled: true, tile: "wide", build: (style) => arrowShapes("credential reuse", "#f08c00", "dashed", style) },
  { key: "arrow-c2", name: "C2 channel", category: "connections", keywords: ["arrow", "beacon", "callback", "command and control"], styled: true, tile: "wide", build: (style) => arrowShapes("C2 channel", "#343a40", "dotted", style) },
  { key: "zone-network", name: "Network zone", category: "zones", keywords: ["subnet", "segment", "vlan", "lan", "internal"], styled: true, tile: "wide", build: (style) => zoneShapes("zone-network", "network", "Internal — 10.0.0.0/24", "#1971c2", style) },
  { key: "zone-dmz", name: "DMZ", category: "zones", keywords: ["perimeter", "subnet", "segment"], styled: true, tile: "wide", build: (style) => zoneShapes("zone-dmz", "globe", "DMZ — 172.16.0.0/24", "#e8590c", style) },
  { key: "zone-cloud", name: "Cloud tenant", category: "zones", keywords: ["aws", "azure", "gcp", "vpc", "subscription"], styled: true, tile: "wide", build: (style) => zoneShapes("zone-cloud", "cloud", "Cloud tenant", "#0c8599", style) },
  { key: "zone-trust", name: "Trust boundary", category: "zones", keywords: ["boundary", "scope", "forest", "domain"], styled: true, tile: "wide", build: (style) => zoneShapes("zone-trust", "shield-check", "Trust boundary", "#e03131", style) },
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

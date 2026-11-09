// Realistic browser User-Agent strings for a chosen OS, browser and version,
// used by the "Generate" helper on fields with format "user_agent", and the
// check under such a field: what a value is, how far behind its browser it
// is, and what in it no real browser sends.
//
// The strings follow what current browsers actually send, not the historical
// variety: Chromium reduced its UA (minor versions "0.0.0", a frozen platform
// token per OS: Windows NT 10.0 even on 11, macOS 10_15_7, Android 10 "K"),
// Firefox and Safari freeze their macOS token, and Safari on iOS 26 and later
// still reports iOS 18_6.
//
// Versions come from each vendor's release dates (user-agent-releases.ts). A
// release that was only scheduled when the tables were read counts two weeks
// after its date, as schedules slip and a version that is not out yet is the
// one thing that makes a UA stand out at once; past a table's end Chrome,
// Edge and Firefox are estimated at two weeks a version, counted the same
// way. The default is the newest version out for a week: on release day
// almost nobody runs it yet.

import {
  CHROME_RELEASES,
  EDGE_RELEASES,
  FIREFOX_ESR,
  FIREFOX_RELEASES,
  RELEASES_AS_OF,
  SAFARI_RELEASES,
  type ReleaseRow,
} from "./user-agent-releases"

export type UAOS = "windows" | "macos" | "linux" | "android" | "ios"
export type UABrowser = "chrome" | "edge" | "firefox" | "safari"

export const UA_OS_LABELS: Record<UAOS, string> = {
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
  android: "Android",
  ios: "iOS (iPhone)",
}

export const UA_BROWSER_LABELS: Record<UABrowser, string> = {
  chrome: "Chrome",
  edge: "Edge",
  firefox: "Firefox",
  safari: "Safari",
}

/** Browsers that ship on each OS, most used first. */
export const UA_BROWSERS_BY_OS: Record<UAOS, UABrowser[]> = {
  windows: ["chrome", "edge", "firefox"],
  macos: ["chrome", "safari", "firefox", "edge"],
  linux: ["chrome", "firefox"],
  android: ["chrome", "firefox"],
  ios: ["safari"],
}

const DAY = 86_400_000
/** How late a release that was only scheduled counts: schedules slip. */
const PLANNED_LAG = 14 * DAY
/** How long a release has been out before it is the default. */
const SETTLED = 7 * DAY
/** Past a table's end: a version every two weeks, as since September 2026. */
const CADENCE = 14 * DAY
/**
 * How long a version stays usual once superseded: browsers that update
 * themselves move on within weeks; Safari moves with the OS, more slowly.
 */
const STALE_AFTER: Record<UABrowser, number> = { chrome: 56 * DAY, edge: 56 * DAY, firefox: 56 * DAY, safari: 112 * DAY }

/** How many versions the generator offers. */
export const UA_VERSION_CHOICES = 4

const TABLES: Record<UABrowser, readonly ReleaseRow[]> = {
  chrome: CHROME_RELEASES,
  edge: EDGE_RELEASES,
  firefox: FIREFOX_RELEASES,
  safari: SAFARI_RELEASES,
}

/** Whether a browser's versions are whole numbers, one a release (all but Safari's x.y). */
const numbered = (b: UABrowser) => b !== "safari"

export interface UARelease {
  version: string
  /** The day it shipped, was scheduled to, or is estimated to (yyyy-mm-dd). */
  date: string
  /** Only scheduled when the tables were read, or estimated past them: it counts two weeks late. */
  planned: boolean
  /** Estimated past the end of the vendor's published schedule. */
  estimated: boolean
}

function day(date: string): number {
  return Date.parse(`${date}T00:00:00Z`)
}

function isoDay(t: number): string {
  return new Date(t).toISOString().slice(0, 10)
}

/** When a release counts as out. */
function countsFrom(r: UARelease): number {
  return day(r.date) + (r.planned ? PLANNED_LAG : 0)
}

/** A browser's releases, oldest first: its table, then estimates up to `until`. */
function schedule(browser: UABrowser, until: number): UARelease[] {
  const asOf = day(RELEASES_AS_OF)
  const out: UARelease[] = TABLES[browser].map(([version, date]) => ({ version, date, planned: day(date) > asOf, estimated: false }))
  const last = out[out.length - 1]
  if (numbered(browser) && last) {
    for (let k = 1; k <= 1000; k++) {
      const t = day(last.date) + k * CADENCE
      if (t > until) break
      out.push({ version: String(Number(last.version) + k), date: isoDay(t), planned: true, estimated: true })
    }
  }
  return out
}

/** A browser's releases out by now, newest first. */
export function uaReleases(browser: UABrowser, now = new Date()): UARelease[] {
  const t = now.getTime()
  return schedule(browser, t)
    .filter((r) => countsFrom(r) <= t)
    .reverse()
}

/** The versions the generator offers for a browser: its newest, newest first. */
export function uaChoices(browser: UABrowser, now = new Date()): UARelease[] {
  return uaReleases(browser, now).slice(0, UA_VERSION_CHOICES)
}

/** The version most of a browser's users run, the newest out for a week: its place among the choices. */
export function defaultBehind(browser: UABrowser, now = new Date()): number {
  const t = now.getTime()
  const i = uaChoices(browser, now).findIndex((r) => t - day(r.date) >= SETTLED)
  return Math.max(0, i)
}

/** How long ago a release came out, for a person: "today", "6 days", "3 weeks", "5 months". */
export function releaseAge(date: string, now = new Date()): string {
  const days = Math.floor((now.getTime() - day(date)) / DAY)
  if (days < 1) return "today"
  if (days < 14) return days === 1 ? "1 day" : `${days} days`
  if (days < 60) return `${Math.floor(days / 7)} weeks`
  return `${Math.floor(days / 30)} months`
}

/** A release's day as the app writes days (yyyy.mm.dd), and what it is. */
export function releaseTitle(r: UARelease): string {
  const when = r.date.replaceAll("-", ".")
  return r.estimated ? `Estimated: ${when}` : r.planned ? `Scheduled for ${when}` : `Released ${when}`
}

export interface UAChoice {
  os: UAOS
  browser: UABrowser
  /** Its place among the browser's choices, newest first (0 = the newest out). */
  behind: number
}

/** The browser version a choice stands for. */
export function uaVersion(choice: UAChoice, now = new Date()): string {
  const choices = uaChoices(choice.browser, now)
  const r = choices[Math.min(Math.max(0, choice.behind), choices.length - 1)]
  return r ? r.version : TABLES[choice.browser][0][0]
}

const CHROMIUM_PLATFORM: Record<Exclude<UAOS, "ios">, string> = {
  windows: "Windows NT 10.0; Win64; x64",
  macos: "Macintosh; Intel Mac OS X 10_15_7",
  linux: "X11; Linux x86_64",
  android: "Linux; Android 10; K",
}

const FIREFOX_PLATFORM: Record<Exclude<UAOS, "ios" | "android">, string> = {
  windows: "Windows NT 10.0; Win64; x64",
  macos: "Macintosh; Intel Mac OS X 10.15",
  linux: "X11; Linux x86_64",
}

/** The iOS version Safari reports for a Safari version (frozen at 18_6 from 26). */
function iosToken(safariMajor: number, minor: number): string {
  return safariMajor >= 26 ? "18_6" : `${safariMajor}_${minor}`
}

/**
 * The User-Agent string for a choice. A browser that does not ship on the OS
 * (e.g. Safari on Windows) falls back to the OS's most used one.
 */
export function generateUserAgent(choice: UAChoice, now = new Date()): string {
  const { os } = choice
  const browser = UA_BROWSERS_BY_OS[os].includes(choice.browser) ? choice.browser : UA_BROWSERS_BY_OS[os][0]
  const version = uaVersion({ ...choice, browser }, now)

  switch (browser) {
    case "chrome":
    case "edge": {
      const platform = CHROMIUM_PLATFORM[os as Exclude<UAOS, "ios">]
      const mobile = os === "android" ? "Mobile " : ""
      const edge = browser === "edge" ? ` Edg/${version}.0.0.0` : ""
      return `Mozilla/5.0 (${platform}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${version}.0.0.0 ${mobile}Safari/537.36${edge}`
    }
    case "firefox": {
      if (os === "android") {
        return `Mozilla/5.0 (Android 15; Mobile; rv:${version}.0) Gecko/${version}.0 Firefox/${version}.0`
      }
      const platform = FIREFOX_PLATFORM[os as Exclude<UAOS, "ios" | "android">]
      return `Mozilla/5.0 (${platform}; rv:${version}.0) Gecko/20100101 Firefox/${version}.0`
    }
    case "safari": {
      const [major, minor] = version.split(".").map(Number)
      if (os === "ios") {
        return `Mozilla/5.0 (iPhone; CPU iPhone OS ${iosToken(major, minor)} like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/${version} Mobile/15E148 Safari/604.1`
      }
      return `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/${version} Safari/605.1.15`
    }
  }
}

// Rough shares of real traffic, so "Random" lands on common combinations.
const WEIGHTS: [UAOS, UABrowser, number][] = [
  ["windows", "chrome", 34],
  ["android", "chrome", 18],
  ["ios", "safari", 14],
  ["windows", "edge", 11],
  ["macos", "chrome", 8],
  ["macos", "safari", 6],
  ["windows", "firefox", 4],
  ["linux", "chrome", 2],
  ["macos", "firefox", 1],
  ["linux", "firefox", 1],
  ["macos", "edge", 1],
]

/** A common choice, weighted by traffic share: mostly the default version, sometimes one or two older. */
export function randomChoice(rand: () => number = Math.random, now = new Date()): UAChoice {
  const total = WEIGHTS.reduce((n, [, , w]) => n + w, 0)
  let r = rand() * total
  let pick = WEIGHTS[0]
  for (const w of WEIGHTS) {
    if (r < w[2]) {
      pick = w
      break
    }
    r -= w[2]
  }
  const [os, browser] = pick
  const age = rand()
  const older = age < 0.6 ? 0 : age < 0.9 ? 1 : 2
  const last = uaChoices(browser, now).length - 1
  return { os, browser, behind: Math.max(0, Math.min(defaultBehind(browser, now) + older, last)) }
}

/** A User-Agent the generator could have made: its OS, browser and version (a major; Safari's x.y). */
export interface ParsedUA {
  os: UAOS
  browser: UABrowser
  version: string
}

/** Browsers the generator does not make, whose strings carry Chrome's, Safari's or Firefox's tokens too. */
const OTHER_BROWSERS = /\b(?:OPR|SamsungBrowser|YaBrowser|Vivaldi|UCBrowser|Electron|EdgA|EdgiOS|CriOS|FxiOS|HeadlessChrome)\/|; wv\)/

function osOf(ua: string): UAOS | null {
  if (/\b(?:iPhone|iPod)\b/.test(ua)) return "ios"
  if (/\bAndroid\b/.test(ua)) return "android"
  if (/\bWindows NT\b/.test(ua)) return "windows"
  if (/\bMacintosh\b|\bMac OS X\b/.test(ua)) return "macos"
  if (/\bLinux\b|\bX11\b/.test(ua)) return "linux"
  return null
}

/** A Safari string's version (x.y), when the string is Safari's. */
function safariVersion(ua: string): string | null {
  if (/\b(?:Chrome|CriOS|FxiOS|EdgiOS|Firefox)\//.test(ua)) return null
  const m = /\bVersion\/(\d+\.\d+)(?:\.\d+)?\b.*\bSafari\//.exec(ua)
  return m ? m[1] : null
}

/** The OS, browser and version of a string the generator makes (an OS and a browser it offers); null for any other. */
export function parseUserAgent(ua: string): ParsedUA | null {
  const s = ua.trim()
  if (!s.startsWith("Mozilla/5.0 (") || OTHER_BROWSERS.test(s)) return null
  const os = osOf(s)
  const edge = /\bEdg\/(\d+)\./.exec(s)
  const firefox = /\bFirefox\/(\d+)\./.exec(s)
  const chrome = /\bChrome\/(\d+)\./.exec(s)
  const safari = safariVersion(s)
  const hit: [UABrowser, string] | null = edge
    ? ["edge", edge[1]]
    : firefox
      ? ["firefox", firefox[1]]
      : chrome
        ? ["chrome", chrome[1]]
        : safari
          ? ["safari", safari]
          : null
  if (!os || !hit || !UA_BROWSERS_BY_OS[os].includes(hit[0])) return null
  return { os, browser: hit[0], version: hit[1] }
}

/** a < b: -1, equal: 0, a > b: 1, by each dot-separated number. */
function compareVersions(a: string, b: string): number {
  const x = a.split(".").map(Number)
  const y = b.split(".").map(Number)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0)
    if (d !== 0) return d < 0 ? -1 : 1
  }
  return 0
}

/**
 * When a version is due: its table's day, or estimated past the table (two
 * weeks a version; Safari's next major in a September, a year after the
 * last). "unknown" for a Safari x.y past the table within a known major.
 */
function dueOf(browser: UABrowser, version: string): { at: number; scheduled: boolean } | "unknown" {
  const table = TABLES[browser]
  const row = table.find(([v]) => v === version)
  if (row) return { at: day(row[1]), scheduled: true }
  const [lastVersion, lastDate] = table[table.length - 1]
  if (numbered(browser)) {
    return { at: day(lastDate) + (Number(version) - Number(lastVersion)) * CADENCE, scheduled: false }
  }
  const major = Number(version.split(".")[0])
  const lastMajor = table.findLast(([v]) => v.endsWith(".0"))
  if (!lastMajor || major <= Number(lastMajor[0].split(".")[0])) return "unknown"
  return { at: day(lastMajor[1]) + (major - Number(lastMajor[0].split(".")[0])) * 365 * DAY, scheduled: false }
}

/** What in a User-Agent no real browser sends, each said for a person. */
function giveaways(ua: string): string[] {
  const out: string[] = []
  if (/\bHeadlessChrome\//.test(ua)) out.push("HeadlessChrome is an automated browser's token: sites and proxies flag it.")
  if (/\bWindows NT 11\.0\b/.test(ua)) out.push("Windows NT 11.0: no browser sends it (on Windows 11 they send 10.0).")
  const chrome = /\bChrome\/(\d+)\.(\d+)\.(\d+)\.(\d+)/.exec(ua)
  if (chrome && Number(chrome[1]) >= 101 && !/; wv\)|\bElectron\//.test(ua) && chrome.slice(2).some((n) => n !== "0")) {
    out.push(`Chrome/${chrome.slice(1).join(".")}: Chrome sends ${chrome[1]}.0.0.0 (since version 101).`)
  }
  if (/\bEdg\/\d/.test(ua) && !/\bChrome\/\d/.test(ua)) out.push("Edg/ without Chrome/: Edge sends both.")
  const os = osOf(ua)
  if (safariVersion(ua) && (os === "windows" || os === "linux")) {
    out.push(`Safari on ${UA_OS_LABELS[os]}: Safari runs on Apple's systems only.`)
  }
  const nt = /\bWindows NT (6\.[0-3])\b/.exec(ua)
  if (nt) {
    // Edge's string carries Chrome's token too: Edge's own first.
    const edge = /\bEdg\/(\d+)\./.exec(ua)
    const chrome = /\bChrome\/(\d+)\./.exec(ua)
    const [name, major] = edge ? ["Edge", edge[1]] : chrome ? ["Chrome", chrome[1]] : ["", "0"]
    if (Number(major) > 109) {
      out.push(`Windows NT ${nt[1]} (Windows 7 or 8) with ${name} ${major}: ${name}'s last there is 109.`)
    }
    const ff = /\bFirefox\/(\d+)\./.exec(ua)
    if (ff && Number(ff[1]) > 115) {
      out.push(`Windows NT ${nt[1]} (Windows 7 or 8) with Firefox ${ff[1]}: Firefox's last there is 115 (ESR).`)
    }
  }
  const rv = /\brv:(\d+)\.\d+\).*\bFirefox\/(\d+)\./.exec(ua)
  if (rv && Number(rv[2]) >= 120 && rv[1] !== rv[2]) {
    out.push(`rv:${rv[1]}.0 with Firefox/${rv[2]}.0: Firefox sends one version in both (since 120).`)
  }
  return out
}

export interface UACheck {
  /** What it is, when it is a string the generator makes. */
  parsed: ParsedUA | null
  /** How many versions of its browser are out newer than it; null when not known. */
  behind: number | null
  /** One of Firefox's extended support releases: what companies run, not behind. */
  esr: boolean
  /** Superseded a while ago: most of that browser's users have moved on. */
  stale: boolean
  /** What in it no real browser sends, each said for a person. */
  findings: string[]
  /**
   * What fixes it: the same OS and browser at the default version (the
   * newest out for a week). Null when there is nothing to fix, or the
   * generator does not make its kind.
   */
  fix: { ua: string; version: string } | null
}

/** Checks a User-Agent: what it is, how current, and its giveaways. */
export function checkUserAgent(ua: string, now = new Date()): UACheck {
  const s = ua.trim()
  const t = now.getTime()
  const findings = giveaways(s)
  const parsed = parseUserAgent(s)
  let behind: number | null = null
  let stale = false
  let esr = false
  let unreleased = false
  if (parsed) {
    const { browser, version } = parsed
    const out = uaReleases(browser, now)
    const latest = out[0]
    if (browser === "firefox" && FIREFOX_ESR.includes(Number(version))) {
      esr = true
    } else if (latest && compareVersions(version, latest.version) > 0) {
      // Newer than any release counted out: out already when its day has
      // passed (within the lag a schedule gets), or not yet.
      const due = dueOf(browser, version)
      if (due !== "unknown" && due.at > t) {
        unreleased = true
        const when = due.scheduled ? `: it is due ${isoDay(due.at).replaceAll("-", ".")}` : ""
        findings.unshift(`${UA_BROWSER_LABELS[browser]} ${version} is not out yet${when}.`)
      } else if (due !== "unknown") {
        behind = 0
      }
    } else if (latest) {
      const newer = out.filter((r) => compareVersions(r.version, version) > 0)
      const oldest = TABLES[browser][0][0]
      behind = numbered(browser)
        ? Number(latest.version) - Number(version)
        : compareVersions(version, oldest) >= 0
          ? newer.length
          : null
      // Its successor: the oldest release newer than it.
      const successor = newer[newer.length - 1]
      stale = successor !== undefined && t - day(successor.date) > STALE_AFTER[browser]
    }
  }
  let fix: UACheck["fix"] = null
  if (parsed && !esr && (stale || unreleased || findings.length > 0)) {
    const choice = { os: parsed.os, browser: parsed.browser, behind: defaultBehind(parsed.browser, now) }
    const ua2 = generateUserAgent(choice, now)
    if (ua2 !== s) fix = { ua: ua2, version: uaVersion(choice, now) }
  }
  return { parsed, behind, esr, stale, findings, fix }
}

/** The line under a User-Agent field: what it is and how current ("Chrome 150 on Windows · 7 versions behind, outdated"). */
export function describeUserAgent(c: UACheck): string | null {
  const p = c.parsed
  if (!p) return null
  const what = `${UA_BROWSER_LABELS[p.browser]} ${p.version} on ${UA_OS_LABELS[p.os]}`
  if (c.esr) return `${what} · an extended support release`
  if (c.behind === 0) return `${what} · latest`
  if (c.behind !== null) {
    return `${what} · ${c.behind} ${c.behind === 1 ? "version" : "versions"} behind${c.stale ? ", outdated" : ""}`
  }
  return c.stale ? `${what} · outdated` : what
}

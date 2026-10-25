// Realistic browser User-Agent strings for a chosen OS, browser and version
// age, used by the "Generate" helper on fields with format "user_agent".
//
// The strings follow what current browsers actually send, not the historical
// variety: Chromium reduced its UA (minor versions "0.0.0", a frozen platform
// token per OS: Windows NT 10.0 even on 11, macOS 10_15_7, Android 10 "K"),
// Firefox and Safari freeze their macOS token, and Safari on iOS 26 still
// reports iOS 18_6.
//
// Versions are estimated from today's date and each browser's release
// cadence, anchored on a known release, so the generator stays current
// without a list to maintain. An estimate can be one version off around a
// release day; real traffic always mixes a few recent versions anyway.

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

/** A release with a known date, and the cadence from it (days per version). */
interface Cadence {
  version: number
  date: string
  days: number
}

// Chromium (Chrome, Edge) and Firefox ship a major version every four weeks.
const CHROMIUM: Cadence = { version: 140, date: "2025-09-02", days: 28 }
const FIREFOX: Cadence = { version: 143, date: "2025-09-16", days: 28 }

function estimate(c: Cadence, now: Date): number {
  const elapsed = now.getTime() - Date.parse(c.date)
  return c.version + Math.max(0, Math.floor(elapsed / (c.days * DAY)))
}

/**
 * Safari: a major version every September (26 in 2025), point releases
 * through the year (x.1 in late autumn ... x.6 by summer).
 */
function safariVersion(now: Date, behind: number): { major: number; minor: number } {
  const year = now.getUTCFullYear()
  // The September release of this year, if it has happened.
  const month = now.getUTCMonth()
  const released = month > 8 || (month === 8 && now.getUTCDate() >= 15) ? year : year - 1
  const major = 26 + (released - 2025)
  const monthsIn = (now.getUTCFullYear() - released) * 12 + now.getUTCMonth() - 8
  let minor = Math.min(6, Math.floor(Math.max(0, monthsIn) / 1.7))
  let m = major
  for (let i = 0; i < behind; i++) {
    if (minor > 0) minor--
    else {
      m--
      minor = 6
    }
  }
  return { major: m, minor }
}

export interface UAChoice {
  os: UAOS
  browser: UABrowser
  /** Versions behind the estimated current one (0 = current). */
  behind: number
}

/** The browser version a choice stands for, as shown in the UI. */
export function uaVersion(choice: UAChoice, now = new Date()): string {
  switch (choice.browser) {
    case "chrome":
    case "edge":
      return String(estimate(CHROMIUM, now) - choice.behind)
    case "firefox":
      return String(estimate(FIREFOX, now) - choice.behind)
    case "safari": {
      const v = safariVersion(now, choice.behind)
      return `${v.major}.${v.minor}`
    }
  }
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

/** A common choice, weighted by traffic share; mostly current, sometimes 1-2 behind. */
export function randomChoice(rand: () => number = Math.random): UAChoice {
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
  const age = rand()
  const behind = age < 0.6 ? 0 : age < 0.9 ? 1 : 2
  return { os: pick[0], browser: pick[1], behind }
}

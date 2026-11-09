import { describe, expect, it } from "vitest"
import {
  checkUserAgent,
  defaultBehind,
  describeUserAgent,
  generateUserAgent,
  parseUserAgent,
  randomChoice,
  releaseAge,
  releaseTitle,
  uaChoices,
  uaReleases,
  UA_BROWSERS_BY_OS,
  type UABrowser,
  type UAOS,
} from "./user-agent"
import { CHROME_RELEASES, EDGE_RELEASES, FIREFOX_RELEASES, RELEASES_AS_OF } from "./user-agent-releases"

const DAY = 86_400_000
// The tables were read on this day: Chrome 157 was 6 days old, Edge 157 4,
// Firefox 159 13, Safari 27.2 14.
const now = new Date("2026-11-09T12:00:00Z")
const versions = (b: UABrowser, at = now) => uaChoices(b, at).map((r) => r.version)

const chromeOn = (platform: string, v: string) =>
  `Mozilla/5.0 (${platform}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${v} Safari/537.36`
const WIN = "Windows NT 10.0; Win64; x64"
const safariMac = (v: string) =>
  `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/${v} Safari/605.1.15`
const firefoxOn = (platform: string, rv: string, v: string) => `Mozilla/5.0 (${platform}; rv:${rv}.0) Gecko/20100101 Firefox/${v}.0`

describe("release tables", () => {
  // Chrome, Edge and Firefox publish months ahead; Safari does not. A
  // failure here is the calendar's, not the code's: read the sources named
  // in user-agent-releases.ts again, extend the tables and move
  // RELEASES_AS_OF to that day.
  it("are fresh enough", () => {
    const today = Date.now()
    for (const [name, rows] of [["Chrome", CHROME_RELEASES], ["Edge", EDGE_RELEASES], ["Firefox", FIREFOX_RELEASES]] as const) {
      const end = rows[rows.length - 1][1]
      expect(Date.parse(end) - today, `${name}'s release table ends ${end}: refresh user-agent-releases.ts`).toBeGreaterThan(90 * DAY)
    }
    expect(today - Date.parse(RELEASES_AS_OF), `the release tables were read ${RELEASES_AS_OF}: refresh user-agent-releases.ts`).toBeLessThan(
      180 * DAY,
    )
  })

  it("go up a version at a time, each a later day", () => {
    for (const rows of [CHROME_RELEASES, EDGE_RELEASES, FIREFOX_RELEASES]) {
      for (let i = 1; i < rows.length; i++) {
        expect(Number(rows[i][0])).toBe(Number(rows[i - 1][0]) + 1)
        expect(rows[i][1] > rows[i - 1][1]).toBe(true)
      }
    }
  })
})

describe("versions", () => {
  it("are the newest released, newest first", () => {
    expect(versions("chrome")).toEqual(["157", "156", "155", "154"])
    expect(versions("edge")).toEqual(["157", "156", "155", "154"])
    expect(versions("firefox")).toEqual(["159", "158", "157", "156"])
    expect(versions("safari")).toEqual(["27.2", "27.1", "27.0", "26.6"])
  })

  it("default to the newest out for a week", () => {
    expect(defaultBehind("chrome", now)).toBe(1) // 157 is 6 days old
    expect(defaultBehind("chrome", new Date("2026-11-10T00:00:00Z"))).toBe(0)
    expect(defaultBehind("edge", now)).toBe(1)
    expect(defaultBehind("firefox", now)).toBe(0)
    expect(defaultBehind("safari", now)).toBe(0)
  })

  it("count a release that was only scheduled two weeks after its day", () => {
    // Firefox 160 was scheduled for Nov 10 when the tables were read.
    expect(versions("firefox", new Date("2026-11-23T23:00:00Z"))[0]).toBe("159")
    expect(versions("firefox", new Date("2026-11-24T00:00:00Z"))[0]).toBe("160")
    expect(versions("chrome", new Date("2026-11-30T12:00:00Z"))[0]).toBe("157")
    expect(versions("chrome", new Date("2026-12-01T00:00:00Z"))[0]).toBe("158")
  })

  it("are estimated past a table, two weeks apart and counted as late", () => {
    const at = new Date("2028-06-01T00:00:00Z")
    const [latest] = uaReleases("chrome", at)
    // 181 was due 2027-10-26: 195 on 2028-05-09, counted from 05-23; 196 not yet.
    expect(latest).toEqual({ version: "195", date: "2028-05-09", planned: true, estimated: true })
    expect(uaReleases("safari", at)[0].version).toBe("27.2")
  })

  it("say their age and day", () => {
    expect(releaseAge("2026-11-09", now)).toBe("today")
    expect(releaseAge("2026-11-08", now)).toBe("1 day")
    expect(releaseAge("2026-11-03", now)).toBe("6 days")
    expect(releaseAge("2026-10-20", now)).toBe("2 weeks")
    expect(releaseAge("2026-07-27", now)).toBe("3 months")
    expect(releaseTitle({ version: "157", date: "2026-11-03", planned: false, estimated: false })).toBe("Released 2026.11.03")
    expect(releaseTitle({ version: "158", date: "2026-11-17", planned: true, estimated: false })).toBe("Scheduled for 2026.11.17")
    expect(releaseTitle({ version: "195", date: "2028-05-09", planned: true, estimated: true })).toBe("Estimated: 2028.05.09")
  })
})

describe("generateUserAgent", () => {
  it("builds reduced Chromium strings", () => {
    expect(generateUserAgent({ os: "windows", browser: "chrome", behind: 1 }, now)).toBe(chromeOn(WIN, "156.0.0.0"))
    expect(generateUserAgent({ os: "android", browser: "chrome", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Mobile Safari/537.36",
    )
    expect(generateUserAgent({ os: "windows", browser: "edge", behind: 0 }, now)).toBe(
      `${chromeOn(WIN, "157.0.0.0")} Edg/157.0.0.0`,
    )
  })

  it("builds Firefox and Safari strings", () => {
    expect(generateUserAgent({ os: "macos", browser: "firefox", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:159.0) Gecko/20100101 Firefox/159.0",
    )
    expect(generateUserAgent({ os: "android", browser: "firefox", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Android 15; Mobile; rv:159.0) Gecko/159.0 Firefox/159.0",
    )
    expect(generateUserAgent({ os: "ios", browser: "safari", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.2 Mobile/15E148 Safari/604.1",
    )
    expect(generateUserAgent({ os: "macos", browser: "safari", behind: 3 }, now)).toBe(safariMac("26.6"))
  })

  it("falls back to a browser the OS ships", () => {
    expect(generateUserAgent({ os: "windows", browser: "safari", behind: 0 }, now)).toContain("Chrome/157")
  })

  it("makes strings the check reads back, with nothing to find", () => {
    for (const [os, browsers] of Object.entries(UA_BROWSERS_BY_OS) as [UAOS, UABrowser[]][]) {
      for (const browser of browsers) {
        for (let behind = 0; behind < 4; behind++) {
          const ua = generateUserAgent({ os, browser, behind }, now)
          const c = checkUserAgent(ua, now)
          expect(c.parsed).toEqual({ os, browser, version: versions(browser)[behind] })
          expect(c.findings).toEqual([])
          expect(c.stale).toBe(false)
          expect(c.fix).toBeNull()
        }
      }
    }
  })
})

describe("randomChoice", () => {
  it("picks valid combinations at or past the default", () => {
    const seq = [0, 0.99, 0.5, 0.7, 0.999, 0.95]
    let i = 0
    const rand = () => seq[i++ % seq.length]
    for (let n = 0; n < 3; n++) {
      const c = randomChoice(rand, now)
      expect(UA_BROWSERS_BY_OS[c.os]).toContain(c.browser)
      expect(c.behind).toBeGreaterThanOrEqual(defaultBehind(c.browser, now))
      expect(c.behind).toBeLessThan(4)
    }
  })
})

describe("parseUserAgent", () => {
  it("leaves browsers the generator does not make", () => {
    for (const ua of [
      `${chromeOn(WIN, "157.0.0.0")} OPR/120.0.0.0`,
      "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/29.0 Chrome/150.0.0.0 Mobile Safari/537.36",
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/157.0.7103.56 Mobile/15E148 Safari/604.1",
      "curl/8.5.0",
      "",
    ]) {
      expect(parseUserAgent(ua)).toBeNull()
    }
  })
})

describe("checkUserAgent", () => {
  const check = (ua: string, at = now) => checkUserAgent(ua, at)

  it("says how far behind, and when outdated, the fix", () => {
    let c = check(chromeOn(WIN, "156.0.0.0"))
    expect([c.behind, c.stale, c.fix]).toEqual([1, false, null])
    expect(describeUserAgent(c)).toBe("Chrome 156 on Windows · 1 version behind")
    expect(describeUserAgent(check(chromeOn(WIN, "157.0.0.0")))).toBe("Chrome 157 on Windows · latest")
    // 153's successor came out 48 days ago; 152's 62: past eight weeks.
    expect(check(chromeOn(WIN, "153.0.0.0")).stale).toBe(false)
    expect(check(chromeOn(WIN, "152.0.0.0")).stale).toBe(true)
    c = check(chromeOn(WIN, "150.0.0.0"))
    expect(describeUserAgent(c)).toBe("Chrome 150 on Windows · 7 versions behind, outdated")
    expect(c.fix).toEqual({ ua: chromeOn(WIN, "156.0.0.0"), version: "156" })
    // Before the tables: behind by the numbers, outdated.
    c = check(chromeOn(WIN, "120.0.0.0"))
    expect([c.behind, c.stale]).toEqual([37, true])
  })

  it("flags a version not out yet", () => {
    let c = check(chromeOn(WIN, "160.0.0.0"))
    expect(c.findings).toEqual(["Chrome 160 is not out yet: it is due 2027.01.05."])
    expect(c.fix?.version).toBe("156")
    expect(describeUserAgent(c)).toBe("Chrome 160 on Windows")
    expect(check(chromeOn(WIN, "300.0.0.0")).findings).toEqual(["Chrome 300 is not out yet."])
    // Its day passed, within the lag a schedule gets: taken as out.
    c = check(chromeOn(WIN, "158.0.0.0"), new Date("2026-11-20T00:00:00Z"))
    expect([c.behind, c.findings]).toEqual([0, []])
    expect(check(chromeOn(WIN, "158.0.0.0")).findings).toEqual(["Chrome 158 is not out yet: it is due 2026.11.17."])
    // Safari: a major before its September.
    c = check(safariMac("28.0"))
    expect(c.findings).toEqual(["Safari 28.0 is not out yet."])
    expect(c.fix?.version).toBe("27.2")
  })

  it("takes a Safari release past the table in its major as unknown, an old one as outdated", () => {
    let c = check(safariMac("27.3"))
    expect([c.behind, c.stale, c.findings, c.fix]).toEqual([null, false, [], null])
    expect(describeUserAgent(c)).toBe("Safari 27.3 on macOS")
    c = check(safariMac("17.4"))
    expect([c.behind, c.stale]).toEqual([null, true])
    expect(describeUserAgent(c)).toBe("Safari 17.4 on macOS · outdated")
    expect(check(safariMac("27.0")).behind).toBe(2)
  })

  it("takes Firefox's extended support releases as current", () => {
    let c = check(firefoxOn(WIN, "153", "153"))
    expect([c.esr, c.stale, c.fix]).toEqual([true, false, null])
    expect(describeUserAgent(c)).toBe("Firefox 153 on Windows · an extended support release")
    c = check(firefoxOn("Windows NT 6.1; Win64; x64", "115", "115"))
    expect([c.esr, c.findings]).toEqual([true, []])
  })

  it("finds what no real browser sends", () => {
    const cases: [string, string][] = [
      [chromeOn("Windows NT 11.0; Win64; x64", "157.0.0.0"), "Windows NT 11.0: no browser sends it (on Windows 11 they send 10.0)."],
      [chromeOn(WIN, "157.0.7103.92"), "Chrome/157.0.7103.92: Chrome sends 157.0.0.0 (since version 101)."],
      [
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36 Edg/157.0.0.0",
        "Edg/ without Chrome/: Edge sends both.",
      ],
      [
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.2 Safari/605.1.15",
        "Safari on Windows: Safari runs on Apple's systems only.",
      ],
      [chromeOn("Windows NT 6.1; Win64; x64", "157.0.0.0"), "Windows NT 6.1 (Windows 7 or 8) with Chrome 157: Chrome's last there is 109."],
      [
        `${chromeOn("Windows NT 6.3; Win64; x64", "120.0.0.0")} Edg/120.0.0.0`,
        "Windows NT 6.3 (Windows 7 or 8) with Edge 120: Edge's last there is 109.",
      ],
      [firefoxOn("Windows NT 6.1; Win64; x64", "159", "159"), "Windows NT 6.1 (Windows 7 or 8) with Firefox 159: Firefox's last there is 115 (ESR)."],
      [firefoxOn(WIN, "109", "159"), "rv:109.0 with Firefox/159.0: Firefox sends one version in both (since 120)."],
      [
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/157.0.7103.92 Safari/537.36",
        "HeadlessChrome is an automated browser's token: sites and proxies flag it.",
      ],
    ]
    for (const [ua, finding] of cases) {
      expect(check(ua).findings, ua).toEqual([finding])
    }
  })

  it("fixes what it can make again, and not what it cannot", () => {
    let c = check(chromeOn("Windows NT 11.0; Win64; x64", "156.0.0.0"))
    expect(c.fix).toEqual({ ua: chromeOn(WIN, "156.0.0.0"), version: "156" })
    // Safari on Windows: no such pair to make.
    c = check("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.2 Safari/605.1.15")
    expect([c.parsed, c.fix]).toEqual([null, null])
    expect(describeUserAgent(c)).toBeNull()
  })

  it("passes what real browsers send", () => {
    for (const ua of [
      // Firefox froze rv: at 109 from 110 to 119.
      firefoxOn(WIN, "109", "118"),
      // Android WebView and Electron keep full Chrome versions.
      "Mozilla/5.0 (Linux; Android 14; Pixel 8 Build/AP1A.240305.019; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/157.0.7103.60 Mobile Safari/537.36",
      `${chromeOn(WIN, "156.0.7103.92")} Electron/39.0.0`,
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.2 Mobile/15E148 Safari/604.1",
    ]) {
      expect(check(ua).findings, ua).toEqual([])
    }
  })
})

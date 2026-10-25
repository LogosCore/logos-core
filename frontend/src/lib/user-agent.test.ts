import { describe, expect, it } from "vitest"
import { generateUserAgent, randomChoice, uaVersion, UA_BROWSERS_BY_OS, type UAOS } from "./user-agent"

const now = new Date("2026-10-25T12:00:00Z")

describe("uaVersion", () => {
  it("estimates from the release cadence", () => {
    expect(uaVersion({ os: "windows", browser: "chrome", behind: 0 }, now)).toBe("154")
    expect(uaVersion({ os: "windows", browser: "chrome", behind: 2 }, now)).toBe("152")
    expect(uaVersion({ os: "windows", browser: "firefox", behind: 0 }, now)).toBe("157")
    expect(uaVersion({ os: "macos", browser: "safari", behind: 0 }, now)).toBe("27.0")
    expect(uaVersion({ os: "macos", browser: "safari", behind: 1 }, now)).toBe("26.6")
  })

  it("matches known releases", () => {
    expect(uaVersion({ os: "windows", browser: "chrome", behind: 0 }, new Date("2025-09-02T12:00:00Z"))).toBe("140")
    expect(uaVersion({ os: "windows", browser: "firefox", behind: 0 }, new Date("2025-10-14T12:00:00Z"))).toBe("144")
    expect(uaVersion({ os: "macos", browser: "safari", behind: 0 }, new Date("2025-09-20T12:00:00Z"))).toBe("26.0")
    expect(uaVersion({ os: "macos", browser: "safari", behind: 0 }, new Date("2025-09-10T12:00:00Z"))).toBe("25.6")
  })
})

describe("generateUserAgent", () => {
  it("builds reduced Chromium strings", () => {
    expect(generateUserAgent({ os: "windows", browser: "chrome", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36",
    )
    expect(generateUserAgent({ os: "android", browser: "chrome", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Mobile Safari/537.36",
    )
    expect(generateUserAgent({ os: "windows", browser: "edge", behind: 1 }, now)).toBe(
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36 Edg/153.0.0.0",
    )
  })

  it("builds Firefox and Safari strings", () => {
    expect(generateUserAgent({ os: "macos", browser: "firefox", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:157.0) Gecko/20100101 Firefox/157.0",
    )
    expect(generateUserAgent({ os: "android", browser: "firefox", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (Android 15; Mobile; rv:157.0) Gecko/157.0 Firefox/157.0",
    )
    expect(generateUserAgent({ os: "ios", browser: "safari", behind: 0 }, now)).toBe(
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.0 Mobile/15E148 Safari/604.1",
    )
  })

  it("falls back to a browser the OS ships", () => {
    expect(generateUserAgent({ os: "windows", browser: "safari", behind: 0 }, now)).toContain("Chrome/154")
  })

  it("covers every OS and browser pair", () => {
    for (const [os, browsers] of Object.entries(UA_BROWSERS_BY_OS)) {
      for (const browser of browsers) {
        const ua = generateUserAgent({ os: os as UAOS, browser, behind: 0 }, now)
        expect(ua).toMatch(/^Mozilla\/5\.0 \(/)
        expect(ua).not.toMatch(/undefined|NaN/)
      }
    }
  })
})

describe("randomChoice", () => {
  it("picks valid combinations", () => {
    const seq = [0, 0.99, 0.5, 0.7, 0.999, 0.95]
    let i = 0
    const rand = () => seq[i++ % seq.length]
    for (let n = 0; n < 3; n++) {
      const c = randomChoice(rand)
      expect(UA_BROWSERS_BY_OS[c.os]).toContain(c.browser)
      expect([0, 1, 2]).toContain(c.behind)
    }
  })
})

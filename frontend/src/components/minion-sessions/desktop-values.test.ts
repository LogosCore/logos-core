import { describe, expect, test } from "vitest"
import {
  clientCommand,
  connectData,
  desktopName,
  desktopURL,
  displayDPI,
  fitScale,
  isDesktopFront,
  requiredLabel,
  requiredSecret,
  statusText,
} from "./desktop-values"

describe("a desktop", () => {
  test("its fronts and names", () => {
    expect(["rdp", "vnc", "ssh", "telnet"].every(isDesktopFront)).toBe(true)
    expect(isDesktopFront("forward") || isDesktopFront("terminal")).toBe(false)
    expect(desktopName("rdp")).toBe("RDP")
    expect(desktopName("telnet")).toBe("Telnet")
  })

  test("its socket, and what it opens with", () => {
    expect(desktopURL("t 1", { protocol: "https:", host: "logos.example:8443" })).toBe("wss://logos.example:8443/api/v1/minion-tasks/t%201/desktop")
    expect(desktopURL("t1", { protocol: "http:", host: "localhost:8080" })).toBe("ws://localhost:8080/api/v1/minion-tasks/t1/desktop")
    expect(connectData({ width: 1280.4, height: 719.6, dpi: 96 }, "Europe/Kyiv")).toBe("width=1280&height=720&dpi=96&timezone=Europe%2FKyiv")
    expect(connectData({ width: 800, height: 600, dpi: 192 })).toBe("width=800&height=600&dpi=192")
    expect(displayDPI(1)).toBe(96)
    expect(displayDPI(2)).toBe(192)
    expect(displayDPI(0)).toBe(96)
  })

  test("a remote display shrinks to fit, never grows", () => {
    expect(fitScale(800, 600, 1600, 1200)).toBe(0.5)
    expect(fitScale(800, 600, 1600, 600)).toBe(0.5)
    expect(fitScale(1920, 1080, 1024, 768)).toBe(1)
    expect(fitScale(800, 600, 0, 0)).toBe(1)
  })

  test("what went wrong, for a person", () => {
    expect(statusText(0x0301)).toBe("The server refused the login")
    expect(statusText(0x0203, "Aborted. See logs.")).toBe("The connection to the server failed")
    expect(statusText(0x0303, "the desktop's login is gone: start the tunnel again")).toBe("the desktop's login is gone: start the tunnel again")
    expect(statusText(0x0999)).toBe("The desktop ended (status 999)")
  })

  test("a login it asks for", () => {
    expect(requiredLabel("domain")).toBe("Domain")
    expect(requiredLabel("some-param")).toBe("some param")
    expect(requiredSecret("password") && requiredSecret("passphrase") && !requiredSecret("username")).toBe(true)
  })

  test("an operator's own client on its port", () => {
    expect(clientCommand("rdp", "core.lan", 24005)).toBe("mstsc /v:core.lan:24005")
    expect(clientCommand("vnc", "core.lan", 24006)).toBe("vncviewer core.lan::24006")
    expect(clientCommand("ssh", "fd00::1", 24007)).toBe("ssh -p 24007 [fd00::1]")
    expect(clientCommand("telnet", "core.lan", 24008)).toBe("telnet core.lan 24008")
  })
})

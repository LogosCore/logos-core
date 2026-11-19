import { describe, expect, test } from "vitest"
import {
  clientCommand,
  clipboardWays,
  connectData,
  desktopName,
  desktopURL,
  displayDPI,
  fitScale,
  followsPane,
  isDesktopFront,
  requiredLabel,
  requiredSecret,
  socketEndText,
  statusText,
  viewOnly,
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
    expect(connectData({ width: 1280.4, height: 719.6, dpi: 96 })).toBe("width=1280&height=720&dpi=96")
    expect(connectData({ width: 800, height: 600, dpi: 192 })).toBe("width=800&height=600&dpi=192")
    // Its settings, a JSON object; none said: core's defaults.
    const q = new URLSearchParams(connectData({ width: 800, height: 600, dpi: 96 }, { quality: "speed", view_only: true }))
    expect(JSON.parse(q.get("settings") ?? "")).toEqual({ quality: "speed", view_only: true })
    expect(connectData({ width: 800, height: 600, dpi: 96 }, {})).toBe("width=800&height=600&dpi=96")
    expect(displayDPI(1)).toBe(96)
    expect(displayDPI(2)).toBe(192)
    expect(displayDPI(0)).toBe(96)
  })

  test("whose screen follows its pane: RDP's pane sizes, and a terminal; not RDP's fixed ones (its default) or VNC's", () => {
    expect(followsPane("rdp", {})).toBe(false)
    expect(followsPane("rdp", { screen: "1920x1080" })).toBe(false)
    expect(followsPane("rdp", { screen: "pane" })).toBe(true)
    expect(followsPane("rdp", { screen: "pane-reconnect" })).toBe(true)
    expect(followsPane("vnc", { screen: "pane" })).toBe(false)
    expect(followsPane("ssh", {})).toBe(true)
    expect(followsPane("telnet", {})).toBe(true)
  })

  test("its settings the view reads: input, and the clipboard's ways", () => {
    expect(viewOnly({ view_only: true })).toBe(true)
    expect(viewOnly({ view_only: false })).toBe(false)
    expect(viewOnly({})).toBe(false)
    expect(clipboardWays({})).toEqual({ fromDesktop: true, toDesktop: true })
    expect(clipboardWays({ clipboard: "both" })).toEqual({ fromDesktop: true, toDesktop: true })
    expect(clipboardWays({ clipboard: "out" })).toEqual({ fromDesktop: true, toDesktop: false })
    expect(clipboardWays({ clipboard: "in" })).toEqual({ fromDesktop: false, toDesktop: true })
    expect(clipboardWays({ clipboard: "off" })).toEqual({ fromDesktop: false, toDesktop: false })
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

  test("its socket's end, unsaid by guacd or core, for a person: the socket's, not the server's", () => {
    // A socket that dropped (Guacamole's tunnel: UPSTREAM_NOT_FOUND), or
    // closed abnormally (SERVER_ERROR).
    expect(socketEndText(0x0207, true)).toBe("The connection to core was lost")
    expect(socketEndText(0x0200, true)).toBe("The connection to core was lost")
    // No word from core for the tunnel's receive timeout.
    expect(socketEndText(0x0202, true)).toBe("Core stopped answering: the connection to it timed out")
    // Before core's first word: the upgrade refused, or core not reached.
    expect(socketEndText(0x0207, false)).toBe("Core could not be reached, or refused the desktop")
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

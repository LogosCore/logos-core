// A desktop task's desktop (docs: contracts/minion-messages/tunnels.md,
// Desktops): core draws it through its guacd, over a WebSocket speaking
// Guacamole's protocol (GET /api/v1/minion-tasks/{id}/desktop), which the
// vendored Guacamole client reads. What is said of it, and how it is
// reached by an operator's own client.

/** The desktops' fronts: a forward to a server of the protocol's, drawn in the web app. */
export const FRONT_RDP = "rdp"
export const FRONT_VNC = "vnc"
export const FRONT_SSH = "ssh"
export const FRONT_TELNET = "telnet"

export function isDesktopFront(front: string): boolean {
  return front === FRONT_RDP || front === FRONT_VNC || front === FRONT_SSH || front === FRONT_TELNET
}

/** A desktop front's name, short: its tab's, its protocol's. */
export function desktopName(front: string): string {
  switch (front) {
    case FRONT_RDP:
      return "RDP"
    case FRONT_VNC:
      return "VNC"
    case FRONT_SSH:
      return "SSH"
    case FRONT_TELNET:
      return "Telnet"
  }
  return front
}

/** A desktop's socket, on this page's host (nginx passes it to core); the client adds its connect data after a "?". */
export function desktopURL(taskId: string, loc: Pick<Location, "protocol" | "host"> = window.location): string {
  const proto = loc.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${loc.host}/api/v1/minion-tasks/${encodeURIComponent(taskId)}/desktop`
}

/**
 * A desktop's settings, as its form gave them, by key: what core's
 * minionDesktopSettings says its front takes (core checks them, and gives
 * each left out its default).
 */
export type DesktopSettings = Readonly<Record<string, unknown>>

/** The settings the view reads itself: whether it takes input, and which way its clipboard goes. */
export const SETTING_VIEW_ONLY = "view_only"
export const SETTING_CLIPBOARD = "clipboard"

/** Whether a desktop is only watched: no key or click reaches its server. */
export function viewOnly(settings: DesktopSettings): boolean {
  return settings[SETTING_VIEW_ONLY] === true
}

/** Which way a desktop's clipboard goes: both by default. */
export function clipboardWays(settings: DesktopSettings): { fromDesktop: boolean; toDesktop: boolean } {
  switch (settings[SETTING_CLIPBOARD]) {
    case "out":
      return { fromDesktop: true, toDesktop: false }
    case "in":
      return { fromDesktop: false, toDesktop: true }
    case "off":
      return { fromDesktop: false, toDesktop: false }
  }
  return { fromDesktop: true, toDesktop: true }
}

/**
 * What a desktop opens with: the display's size and density, the browser's
 * time zone (the session's), and its settings (a JSON object; none: core's
 * defaults).
 */
export function connectData(
  size: { width: number; height: number; dpi: number },
  timezone?: string,
  settings?: DesktopSettings,
): string {
  const q = new URLSearchParams({
    width: String(Math.round(size.width)),
    height: String(Math.round(size.height)),
    dpi: String(Math.round(size.dpi)),
  })
  if (timezone) q.set("timezone", timezone)
  if (settings && Object.keys(settings).length > 0) q.set("settings", JSON.stringify(settings))
  return q.toString()
}

/** The display's density for a device pixel ratio: 96 a CSS pixel, as Guacamole reckons it. */
export function displayDPI(devicePixelRatio: number): number {
  return Math.round(96 * Math.max(1, devicePixelRatio || 1))
}

/** How far a remote display of w×h is shrunk to fit a box: never grown. */
export function fitScale(boxWidth: number, boxHeight: number, width: number, height: number): number {
  if (width <= 0 || height <= 0 || boxWidth <= 0 || boxHeight <= 0) return 1
  return Math.min(boxWidth / width, boxHeight / height, 1)
}

// Guacamole's status codes (Guacamole.Status.Code), which the client's errors carry.
const STATUS_TEXT: Record<number, string> = {
  0x0100: "The server does not support what was asked",
  0x0200: "guacd failed",
  0x0201: "guacd is too busy",
  0x0202: "The server did not answer in time",
  0x0203: "The connection to the server failed",
  0x0204: "Core has no such desktop now",
  0x0207: "The server could not be reached",
  0x0208: "The server is unavailable",
  0x0209: "Another session took this one's place",
  0x020a: "The session timed out",
  0x020b: "The server ended the session",
  0x0300: "Core refused the request",
  0x0301: "The server refused the login",
  0x0303: "Not allowed",
  0x0308: "The session timed out with no input",
  0x031d: "Too many sessions",
}

/** What went wrong, for a person: guacd's or core's message where it says one, else the status's. */
export function statusText(code: number, message?: string | null): string {
  const known = STATUS_TEXT[code]
  const said = message?.trim()
  // guacd's generic one says nothing of use.
  if (said && said !== "Aborted. See logs.") return known && said.length < 12 ? `${known}: ${said}` : said
  return known ?? `The desktop ended (status ${code.toString(16)})`
}

/** A connection param the server asks for, for a person. */
export function requiredLabel(name: string): string {
  switch (name) {
    case "username":
      return "Username"
    case "password":
      return "Password"
    case "domain":
      return "Domain"
    case "passphrase":
      return "Key passphrase"
  }
  return name.replaceAll("-", " ")
}

/** Whether a param the server asks for is a secret: typed hidden. */
export function requiredSecret(name: string): boolean {
  return name === "password" || name === "passphrase"
}

/** Ctrl+Alt+Del's keysyms, pressed in order, released in reverse. */
export const CTRL_ALT_DEL = [0xffe3, 0xffe9, 0xffff] as const

/** The command an operator's own client reaches a desktop's port with. */
export function clientCommand(front: string, host: string, port: number): string {
  switch (front) {
    case FRONT_RDP:
      return `mstsc /v:${hostPortOf(host, port)}`
    case FRONT_VNC:
      return `vncviewer ${bracketed(host)}::${port}`
    case FRONT_SSH:
      return `ssh -p ${port} ${bracketed(host)}`
    case FRONT_TELNET:
      return `telnet ${host} ${port}`
  }
  return hostPortOf(host, port)
}

function bracketed(host: string): string {
  return host.includes(":") && !host.startsWith("[") ? `[${host}]` : host
}

function hostPortOf(host: string, port: number): string {
  return `${bracketed(host)}:${port}`
}

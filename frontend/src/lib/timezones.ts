// Time zones as IANA names them (Europe/Berlin), for a field that holds one
// (protocol.FormatTimezone): the browser's own list, each with its offset
// now, matched by any part of the name as it is typed. Nothing shipped:
// Intl knows them.

export interface Timezone {
  /** Its IANA name: Europe/Kyiv. */
  name: string
  /** Its offset from UTC now: UTC+2, UTC-3:30, UTC. */
  offset: string
}

/** The time zones the browser knows, UTC among them, by name. */
export function timezoneNames(): string[] {
  let names: string[] = []
  try {
    names = Intl.supportedValuesOf("timeZone")
  } catch {
    // An older browser: the browser's own zone alone, below.
  }
  const own = browserTimezone()
  const all = new Set(names)
  all.add("UTC")
  if (own) all.add(own)
  return [...all].sort()
}

/** The browser's own time zone, or "" when it says none. */
export function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? ""
  } catch {
    return ""
  }
}

/** A time zone's offset from UTC at the time given: UTC+2, UTC-3:30, UTC. */
export function timezoneOffset(name: string, at: Date): string {
  try {
    const part = new Intl.DateTimeFormat("en-US", { timeZone: name, timeZoneName: "shortOffset" })
      .formatToParts(at)
      .find((p) => p.type === "timeZoneName")?.value
    // "GMT+2", "GMT-3:30", "GMT" (some engines "GMT+0").
    if (part?.startsWith("GMT")) {
      const offset = part.slice(3)
      return offset === "" || offset === "+0" || offset === "-0" ? "UTC" : "UTC" + offset
    }
  } catch {
    // A name this browser does not know.
  }
  return ""
}

/** What a name or query is matched as: lower case, its parts by spaces. */
function words(s: string): string {
  return s.toLowerCase().replace(/[_/]+/g, " ").replace(/\s+/g, " ").trim()
}

/**
 * The time zones a query names, at most limit: those whose name has it
 * (any part: "kyiv", "new y", "america/new"), or whose offset is it
 * ("utc+2"); the browser's own zone first, then those whose city starts
 * with it, then the rest by name. An empty query: all, the browser's first.
 */
export function matchTimezones(zones: readonly Timezone[], query: string, own: string, limit = 50): Timezone[] {
  const q = words(query)
  const offsetQuery = query.trim().toUpperCase().replace(/^GMT/, "UTC")
  const rank = (z: Timezone): number => {
    if (z.name === own) return 0
    if (q === "") return 1
    const city = words(z.name.slice(z.name.lastIndexOf("/") + 1))
    return city.startsWith(q) ? 1 : 2
  }
  return zones
    .filter((z) => q === "" || words(z.name).includes(q) || (offsetQuery.startsWith("UTC") && z.offset === offsetQuery))
    .map((z) => ({ z, r: rank(z) }))
    .sort((a, b) => a.r - b.r || a.z.name.localeCompare(b.z.name))
    .slice(0, limit)
    .map(({ z }) => z)
}

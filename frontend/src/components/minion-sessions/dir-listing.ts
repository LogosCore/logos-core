// A dir_listing task's data as the console's file browser reads it (docs:
// contracts/task-result-handlers.md): the directory, how the host's paths
// join, and its entries. Paths are the host's: they are split and joined
// by the separator the minion gave, never guessed.

export interface DirEntry {
  name: string
  /** dir, file, link or other; a type this build does not know reads as other. */
  type: "dir" | "file" | "link" | "other"
  size?: number
  /** Unix seconds. */
  modified?: number
  mode?: string
  owner?: string
  group?: string
  target?: string
  hidden?: boolean
}

export interface DirListing {
  path: string
  sep: "/" | "\\"
  parent?: string
  entries: DirEntry[]
}

const TYPES = new Set(["dir", "file", "link", "other"])

/** The data as a listing, or null when it is not one (shown raw then). */
export function readDirListing(data: unknown): DirListing | null {
  if (typeof data !== "object" || data === null) return null
  const d = data as Record<string, unknown>
  if (typeof d.path !== "string" || (d.sep !== "/" && d.sep !== "\\") || !Array.isArray(d.entries)) return null
  const entries: DirEntry[] = []
  for (const e of d.entries) {
    if (typeof e !== "object" || e === null || typeof (e as DirEntry).name !== "string") return null
    const x = e as Record<string, unknown>
    entries.push({
      name: x.name as string,
      type: typeof x.type === "string" && TYPES.has(x.type) ? (x.type as DirEntry["type"]) : "other",
      size: typeof x.size === "number" ? x.size : undefined,
      modified: typeof x.modified === "number" ? x.modified : undefined,
      mode: typeof x.mode === "string" ? x.mode : undefined,
      owner: typeof x.owner === "string" ? x.owner : undefined,
      group: typeof x.group === "string" ? x.group : undefined,
      target: typeof x.target === "string" ? x.target : undefined,
      hidden: x.hidden === true,
    })
  }
  return { path: d.path, sep: d.sep, parent: typeof d.parent === "string" && d.parent !== "" ? d.parent : undefined, entries }
}

/** An entry's path in the directory. */
export function childPath(l: Pick<DirListing, "path" | "sep">, name: string): string {
  return l.path.endsWith(l.sep) ? l.path + name : l.path + l.sep + name
}

export interface Crumb {
  label: string
  path: string
}

/**
 * The directories of a path, from its root: "/usr/bin" is /, usr, bin;
 * "C:\Users\ann" is C:\, Users, ann; "\\srv\share\x" is \\srv\share\, x.
 */
export function crumbs(path: string, sep: "/" | "\\"): Crumb[] {
  let root: string
  if (sep === "\\" && path.startsWith("\\\\")) {
    // A share: \\server\share\ is its root.
    const [server = "", share = ""] = path.slice(2).split("\\")
    root = `\\\\${server}\\${share}\\`
  } else {
    const at = path.indexOf(sep)
    root = at < 0 ? path : path.slice(0, at + 1)
  }
  const out: Crumb[] = [{ label: root, path: root }]
  let at = root
  for (const name of path.slice(root.length).split(sep).filter((s) => s !== "")) {
    at = at.endsWith(sep) ? at + name : at + sep + name
    out.push({ label: name, path: at })
  }
  return out
}

/** Folders first (a link may lead to one: with them), then by name. */
export function sortEntries(entries: readonly DirEntry[]): DirEntry[] {
  const rank = (e: DirEntry) => (e.type === "dir" ? 0 : e.type === "link" ? 1 : 2)
  return [...entries].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name))
}

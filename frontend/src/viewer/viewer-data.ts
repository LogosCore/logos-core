// The HTML bundle's data, as core/pkg/wikitransfer/htmlbundle writes it, and
// the functions that load it.
//
// The bundle is opened from disk, and a file:// page may not fetch() — every
// browser refuses. A <script> tag is the one way to read another local file,
// so each data file is a script that calls a global installed by
// index.html: window.__logosExport(tree) and window.__logosPage(id, body).

import type { JSONContent } from "@tiptap/core"

/** Bumped by the exporter with any change an older viewer would misread. */
export const SUPPORTED_VERSION = 1

export interface ExportPage {
  id: string
  parentId?: string
  title: string
  emoji?: string
  icon?: string
  color?: string
  kind: "document" | "drawing"
  template?: boolean
  /** Plain text, for search. */
  text?: string
  /** The body could not be exported; the page is in the tree regardless. */
  missing?: boolean
}

export interface ExportTree {
  version: number
  title: string
  operation: string
  scope: "tree" | "subtree"
  exportedAt: string
  pages: ExportPage[]
}

export interface ExcalidrawFileData {
  type: string
  elements: unknown[]
  appState: Record<string, unknown>
  files: Record<string, unknown>
}

export type PageBody =
  | { kind: "document"; doc: JSONContent }
  | {
      kind: "drawing"
      scene: ExcalidrawFileData
      /** The records the drawing's record cards stand for, by "kind:id":
       * the exporter's chip data plus `kind`, or `missing`. */
      refs?: Record<string, Record<string, unknown>>
    }

declare global {
  interface Window {
    __logosTree?: ExportTree
    __logosPages?: Record<string, PageBody>
    __logosPage?: (id: string, body: PageBody) => void
  }
}

/** The tree, read once from the global data/tree.js set before this script. */
export function readTree(): ExportTree | null {
  return window.__logosTree ?? null
}

const pending = new Map<string, Promise<PageBody>>()

/** Loads one page's body by adding its script, once. */
export function loadPage(id: string): Promise<PageBody> {
  const cached = window.__logosPages?.[id]
  if (cached) return Promise.resolve(cached)
  const inflight = pending.get(id)
  if (inflight) return inflight

  const promise = new Promise<PageBody>((resolve, reject) => {
    const script = document.createElement("script")
    script.src = `data/pages/${encodeURIComponent(id)}.js`
    script.async = true
    script.onload = () => {
      const body = window.__logosPages?.[id]
      if (body) resolve(body)
      else reject(new Error("The page's data file did not define it."))
    }
    script.onerror = () => {
      pending.delete(id)
      reject(new Error("The page's data file is missing from the export."))
    }
    document.head.appendChild(script)
  })
  pending.set(id, promise)
  return promise
}

/** Index of the tree for the sidebar: children in export order. */
export interface TreeIndex {
  byId: Map<string, ExportPage>
  roots: ExportPage[]
  children: Map<string, ExportPage[]>
}

export function indexTree(pages: ExportPage[]): TreeIndex {
  const byId = new Map(pages.map((p) => [p.id, p]))
  const roots: ExportPage[] = []
  const children = new Map<string, ExportPage[]>()
  for (const p of pages) {
    if (p.parentId && byId.has(p.parentId)) {
      const list = children.get(p.parentId)
      if (list) list.push(p)
      else children.set(p.parentId, [p])
    } else {
      roots.push(p)
    }
  }
  return { byId, roots, children }
}

/** The page's ancestors, root first. */
export function ancestorsOf(index: TreeIndex, id: string): ExportPage[] {
  const out: ExportPage[] = []
  const seen = new Set<string>()
  let cur = index.byId.get(id)?.parentId
  while (cur && !seen.has(cur)) {
    seen.add(cur)
    const page = index.byId.get(cur)
    if (!page) break
    out.unshift(page)
    cur = page.parentId
  }
  return out
}

export interface SearchHit {
  page: ExportPage
  /** Text around the first match in the body; empty for a title-only hit. */
  snippet: string
  titleMatch: boolean
}

/**
 * Full-text search over titles and bodies. Every word of the query must
 * appear somewhere in the page; title matches rank first, then the tree's
 * own order.
 */
export function search(pages: ExportPage[], query: string, limit = 50): SearchHit[] {
  const words = queryWords(query)
  if (words.length === 0) return []
  const titleHits: SearchHit[] = []
  const bodyHits: SearchHit[] = []
  for (const page of pages) {
    const title = page.title.toLowerCase()
    const text = (page.text ?? "").toLowerCase()
    if (!words.every((w) => title.includes(w) || text.includes(w))) continue
    const titleMatch = words.every((w) => title.includes(w))
    const at = words.map((w) => text.indexOf(w)).filter((i) => i >= 0)
    const snippet = at.length ? snippetAround(page.text ?? "", Math.min(...at)) : ""
    ;(titleMatch ? titleHits : bodyHits).push({ page, snippet, titleMatch })
  }
  return [...titleHits, ...bodyHits].slice(0, limit)
}

function snippetAround(text: string, at: number, radius = 60): string {
  const start = Math.max(0, at - radius)
  const end = Math.min(text.length, at + radius)
  const body = text.slice(start, end).replace(/\s+/g, " ").trim()
  return (start > 0 ? "… " : "") + body + (end < text.length ? " …" : "")
}

/** The words of a search query, as search() matches them. */
export function queryWords(query: string): string[] {
  return query.toLowerCase().split(/\s+/).filter(Boolean)
}

/**
 * Where any of the words occur in text, case-insensitively, as sorted,
 * non-overlapping [start, end) ranges — overlapping and touching matches are
 * merged, so "edge" and "dge-ho" in "edge-homed" highlight as one run.
 */
export function matchRanges(text: string, words: string[]): [number, number][] {
  const lower = text.toLowerCase()
  const found: [number, number][] = []
  for (const w of words) {
    if (!w) continue
    for (let i = lower.indexOf(w); i >= 0; i = lower.indexOf(w, i + 1)) found.push([i, i + w.length])
  }
  found.sort((a, b) => a[0] - b[0] || a[1] - b[1])
  const merged: [number, number][] = []
  for (const r of found) {
    const last = merged.at(-1)
    if (last && r[0] <= last[1]) last[1] = Math.max(last[1], r[1])
    else merged.push([r[0], r[1]])
  }
  return merged
}

/** `#/page/<id>` → id. */
export function pageIdFromHash(hash: string): string | null {
  const m = /^#\/page\/([^/?#]+)/.exec(hash)
  return m ? decodeURIComponent(m[1]) : null
}

export function hashForPage(id: string): string {
  return `#/page/${encodeURIComponent(id)}`
}

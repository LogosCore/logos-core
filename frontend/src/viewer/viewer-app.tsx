import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ChevronRightIcon,
  MenuIcon,
  MonitorIcon,
  MoonIcon,
  SearchIcon,
  SunIcon,
  XIcon,
} from "lucide-react"
import { useTheme } from "next-themes"
import { cn } from "@/lib/utils"
import { DocumentIcon } from "@/components/wiki/document-icon"
import { wikiRowIndent } from "@/components/wiki/wiki-tree-helpers"
import { ResizeHandle } from "@/components/wiki/resize-handle"
import {
  ancestorsOf,
  hashForPage,
  indexTree,
  pageIdFromHash,
  queryWords,
  search,
  type ExportPage,
  type ExportTree,
  type TreeIndex,
} from "./viewer-data"
import { Toaster } from "@/components/ui/sonner"
import { ViewerPageBody } from "./viewer-page"
import { Highlighted } from "./viewer-highlight"
import { formatDateTime } from "@/lib/date-format"

// Per-viewer conveniences only. Storage may be unavailable (some browsers
// block it for file:// pages), so every access is guarded and nothing
// depends on it.
function readStored<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

function writeStored(key: string, value: unknown) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Not available; the setting lasts for this visit.
  }
}

type ThemeChoice = "system" | "light" | "dark"

/** The app's theme (next-themes, see main.tsx), cycled by one button. */
function useViewerTheme(): [ThemeChoice, boolean, () => void] {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const choice: ThemeChoice = theme === "light" || theme === "dark" ? theme : "system"
  const cycle = useCallback(() => {
    setTheme(choice === "system" ? "light" : choice === "light" ? "dark" : "system")
  }, [choice, setTheme])
  return [choice, resolvedTheme === "dark", cycle]
}

function useRoute(index: TreeIndex, onRoute: (id: string) => void): [string | null, (id: string) => void] {
  const first = index.roots[0]?.id ?? null
  const read = useCallback(() => {
    const id = pageIdFromHash(window.location.hash)
    return id && index.byId.has(id) ? id : first
  }, [index, first])
  const [current, setCurrent] = useState<string | null>(read)
  useEffect(() => {
    const onHash = () => {
      const id = read()
      setCurrent(id)
      if (id) onRoute(id)
    }
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [read, onRoute])
  const go = useCallback((id: string) => {
    window.location.hash = hashForPage(id)
  }, [])
  return [current, go]
}

export function ViewerApp({ tree }: { tree: ExportTree }) {
  const index = useMemo(() => indexTree(tree.pages), [tree])
  const [themeChoice, dark, cycleTheme] = useViewerTheme()
  const [query, setQuery] = useState("")
  const [sidebarOpen, setSidebarOpen] = useState(false)
  // Draggable, as in the app: the handle moves `--wiki-sidebar-width` on the
  // element while dragging and commits once on release.
  const sidebarRef = useRef<HTMLDivElement | null>(null)
  const [sidebarWidth, setSidebarWidth] = useState(() => readStored("logos-viewer-sidebar-width", 288))
  const commitSidebarWidth = useCallback((width: number) => {
    setSidebarWidth(width)
    writeStored("logos-viewer-sidebar-width", width)
  }, [])

  const expandKey = `logos-viewer-expanded:${tree.title}:${tree.exportedAt}`
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const initial = new Set(readStored<string[]>(expandKey, []))
    // The page a link opened is shown in its place in the tree.
    const id = pageIdFromHash(window.location.hash)
    if (id) for (const a of ancestorsOf(index, id)) initial.add(a.id)
    return initial
  })
  const toggle = useCallback(
    (id: string) =>
      setExpanded((prev) => {
        const next = new Set(prev)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        writeStored(expandKey, [...next])
        return next
      }),
    [expandKey],
  )
  // Every navigation — the tree, search, a link in a page — expands the
  // destination's ancestors, so the tree shows where it landed.
  const reveal = useCallback(
    (id: string) =>
      setExpanded((prev) => {
        const missing = ancestorsOf(index, id).filter((a) => !prev.has(a.id))
        if (missing.length === 0) return prev
        const next = new Set(prev)
        for (const a of missing) next.add(a.id)
        return next
      }),
    [index],
  )
  const [current, go] = useRoute(index, reveal)
  const page = current ? index.byId.get(current) : undefined
  const ancestors = useMemo(() => (current ? ancestorsOf(index, current) : []), [index, current])

  useEffect(() => {
    document.title = page ? `${page.title || "Untitled"} — ${tree.title}` : tree.title
  }, [page, tree.title])

  const open = (id: string) => {
    go(id)
    setSidebarOpen(false)
  }

  const searching = query.trim().length >= 2
  const hits = useMemo(() => (searching ? search(tree.pages, query) : null), [tree.pages, query, searching])
  // What the open page highlights: the search's words, while it is active.
  const highlight = useMemo(() => (searching ? queryWords(query) : []), [query, searching])
  const ThemeIcon = themeChoice === "system" ? MonitorIcon : themeChoice === "light" ? SunIcon : MoonIcon

  return (
    <div className="flex h-svh min-h-0 gap-1 overflow-hidden bg-background p-2 text-foreground">
      {/* Copy confirmations, from the chip dialogs and code blocks. */}
      <Toaster />
      <aside
        ref={sidebarRef}
        style={{ ["--wiki-sidebar-width" as string]: `${sidebarWidth}px` } as React.CSSProperties}
        className={cn(
          "flex shrink-0 flex-col overflow-hidden rounded-lg border bg-card md:w-(--wiki-sidebar-width)",
          "max-md:fixed max-md:inset-2 max-md:z-30 max-md:w-auto",
          !sidebarOpen && "max-md:hidden",
        )}
      >
        <div className="flex items-start gap-2 border-b px-3 py-2">
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{tree.title}</div>
            <div className="truncate text-xs text-muted-foreground">
              {tree.operation && tree.operation !== tree.title ? `${tree.operation} · ` : ""}
              Exported {formatDateTime(tree.exportedAt)}
            </div>
          </div>
          <button
            type="button"
            onClick={cycleTheme}
            className="flex size-7 shrink-0 items-center justify-center rounded-md hover:bg-muted"
            title={`Theme: ${themeChoice}`}
            aria-label={`Theme: ${themeChoice}`}
          >
            <ThemeIcon className="size-4" />
          </button>
          <button
            type="button"
            onClick={() => setSidebarOpen(false)}
            className="flex size-7 shrink-0 items-center justify-center rounded-md hover:bg-muted md:hidden"
            aria-label="Close"
          >
            <XIcon className="size-4" />
          </button>
        </div>
        <div className="border-b p-2">
          <label className="flex h-8 items-center gap-2 rounded-md border bg-background px-2 text-sm focus-within:ring-2 focus-within:ring-ring/40">
            <SearchIcon className="size-4 shrink-0 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search pages"
              className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
            />
            {query && (
              <button type="button" onClick={() => setQuery("")} aria-label="Clear search">
                <XIcon className="size-3.5 text-muted-foreground" />
              </button>
            )}
          </label>
        </div>
        <nav className="flex-1 overflow-y-auto px-1 py-1">
          {hits ? (
            hits.length === 0 ? (
              <p className="px-2 py-4 text-center text-xs text-muted-foreground">No pages match.</p>
            ) : (
              hits.map((hit) => (
                <button
                  key={hit.page.id}
                  type="button"
                  onClick={() => open(hit.page.id)}
                  className={cn(
                    "flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-muted",
                    hit.page.id === current && "bg-accent text-accent-foreground",
                  )}
                >
                  <span className="flex items-center gap-1.5 text-sm">
                    <PageIcon page={hit.page} />
                    <span className="truncate">
                      <Highlighted text={hit.page.title || "Untitled"} words={highlight} />
                    </span>
                  </span>
                  {hit.snippet && (
                    <span className="line-clamp-2 text-xs text-muted-foreground">
                      <Highlighted text={hit.snippet} words={highlight} />
                    </span>
                  )}
                </button>
              ))
            )
          ) : (
            <TreeLevel
              pages={index.roots}
              depth={0}
              index={index}
              expanded={expanded}
              current={current}
              onToggle={toggle}
              onOpen={open}
            />
          )}
        </nav>
      </aside>

      <div className="flex max-md:hidden">
        <ResizeHandle currentWidth={sidebarWidth} onResize={commitSidebarWidth} sidebarRef={sidebarRef} />
      </div>

      <main className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border bg-card">
        <header className="flex h-10 shrink-0 items-center gap-1 border-b px-3">
          <button
            type="button"
            onClick={() => setSidebarOpen(true)}
            className="mr-1 flex size-7 items-center justify-center rounded-md hover:bg-muted md:hidden"
            aria-label="Pages"
          >
            <MenuIcon className="size-4" />
          </button>
          {page && (
            <>
              {ancestors.map((a) => (
                <span key={a.id} className="flex min-w-0 items-center gap-1 max-sm:hidden">
                  <button
                    type="button"
                    onClick={() => open(a.id)}
                    className="flex min-w-0 items-center gap-1 rounded px-1 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
                  >
                    <PageIcon page={a} hasChildren />
                    <span className="max-w-40 truncate">{a.title || "Untitled"}</span>
                  </button>
                  <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground" />
                </span>
              ))}
              <span className="flex min-w-0 items-center gap-1.5">
                <PageIcon page={page} size={18} hasChildren={index.children.has(page.id)} />
                <span className="truncate text-sm font-medium">{page.title || "Untitled"}</span>
              </span>
            </>
          )}
        </header>
        {/* A drawing gets the whole pane as its canvas; prose scrolls. */}
        <div
          className={cn(
            "min-h-0 flex-1",
            page?.kind === "drawing" && !page.missing ? "overflow-hidden p-2" : "overflow-y-auto py-2 pr-4 pl-6",
          )}
        >
          {page ? (
            <ViewerPageBody page={page} dark={dark} highlight={highlight} />
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">This export holds no pages.</p>
          )}
        </div>
      </main>
    </div>
  )
}

/** A whole-screen message, for an export the viewer cannot show. */
export function ViewerProblem({ message }: { message: string }) {
  return (
    <div className="flex h-svh items-center justify-center bg-background p-6 text-center text-sm text-muted-foreground">
      {message}
    </div>
  )
}

function PageIcon({ page, size, hasChildren }: { page: ExportPage; size?: number; hasChildren?: boolean }) {
  return (
    <DocumentIcon
      emoji={page.emoji}
      icon={page.icon}
      color={page.color}
      size={size}
      hasChildren={hasChildren}
      isExpanded
      isTemplate={page.template}
      isDrawing={page.kind === "drawing"}
    />
  )
}

interface TreeLevelProps {
  pages: ExportPage[]
  depth: number
  index: TreeIndex
  expanded: Set<string>
  current: string | null
  onToggle: (id: string) => void
  onOpen: (id: string) => void
}

function TreeLevel({ pages, depth, index, expanded, current, onToggle, onOpen }: TreeLevelProps) {
  return (
    <>
      {pages.map((page) => {
        const kids = index.children.get(page.id) ?? []
        const isOpen = expanded.has(page.id)
        return (
          <div key={page.id}>
            <div
              style={{ paddingLeft: wikiRowIndent(depth) }}
              className={cn(
                "group flex h-7 items-center gap-0.5 rounded-md px-1 text-sm",
                page.id === current ? "bg-accent text-accent-foreground" : "hover:bg-muted",
              )}
            >
              <span className="flex size-5 shrink-0 items-center justify-center">
                {kids.length > 0 ? (
                  <button
                    type="button"
                    onClick={() => onToggle(page.id)}
                    className="flex size-5 shrink-0 items-center justify-center rounded hover:bg-muted-foreground/10"
                    aria-label={isOpen ? "Collapse" : "Expand"}
                    aria-expanded={isOpen}
                  >
                    <span className="flex size-5 items-center justify-center text-sm group-hover:hidden">
                      <DocumentIcon
                        emoji={page.emoji}
                        icon={page.icon}
                        color={page.color}
                        hasChildren
                        isExpanded={isOpen}
                        isTemplate={page.template}
                        isDrawing={page.kind === "drawing"}
                      />
                    </span>
                    <ChevronRightIcon
                      className={cn("hidden size-3.5 transition-transform group-hover:block", isOpen && "rotate-90")}
                    />
                  </button>
                ) : (
                  <PageIcon page={page} />
                )}
              </span>
              <a
                href={hashForPage(page.id)}
                onClick={(e) => {
                  e.preventDefault()
                  onOpen(page.id)
                }}
                className={cn(
                  "flex h-full flex-1 items-center truncate px-1 text-left text-sm",
                  page.missing && "text-muted-foreground line-through",
                )}
              >
                {page.title || "Untitled"}
              </a>
            </div>
            {isOpen && kids.length > 0 && (
              <TreeLevel
                pages={kids}
                depth={depth + 1}
                index={index}
                expanded={expanded}
                current={current}
                onToggle={onToggle}
                onOpen={onOpen}
              />
            )}
          </div>
        )
      })}
    </>
  )
}

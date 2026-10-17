// The Shapes panel: the built-in shapes and the operator's own, in a sidebar
// of the canvas.
//
// Replaces Excalidraw's library panel for picking shapes. That panel shows
// every item as a thumbnail four to a row with no names, so a card read as a
// coloured smudge and picking one meant guessing. Here every shape has a
// large preview with its name, shapes are grouped by what they are, search
// matches names and keywords ("dc" finds the domain controller, "pwned" the
// compromised badge), and one switch picks the style instead of every shape
// appearing twice.
//
// Built on Excalidraw's own Sidebar, so it opens, docks and closes like the
// native one. A click inserts the shape at the centre of the view, selected
// and undoable (drawing-insert.ts). Dragging hands the canvas a library item,
// the same thing Excalidraw's own panel does, and the canvas places it where
// it is dropped.
//
// "Add to library" in Excalidraw's own menus still adds to the operator's
// library (drawing-library.ts). Those shapes show here under "My shapes".

import { useEffect, useMemo, useRef, useState } from "react"
import {
  CaptureUpdateAction,
  MIME_TYPES,
  Sidebar,
  convertToExcalidrawElements,
  exportToSvg,
  serializeLibraryAsJSON,
} from "@excalidraw/excalidraw"
import type { ExcalidrawImperativeAPI, LibraryItem, LibraryItems } from "@excalidraw/excalidraw/types"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import { ChevronDown, Search, Shapes, X } from "lucide-react"

import { cn } from "@/lib/utils"
import {
  BUILTIN_SHAPES,
  SHAPE_CATEGORIES,
  matchesShape,
  type BuiltinShape,
  type ShapeStyle,
} from "./drawing-builtin-library"
import { centeredAt, randomId, viewportCenter, withFreshIds } from "./drawing-insert"

export const SHAPES_SIDEBAR = "logos-shapes"

// ---------------------------------------------------------------------------
// Preferences: style, collapsed categories, docked. Per browser, like the
// grid preference.

const PREFS_KEY = "wiki_drawing_shapes_panel"

interface Prefs {
  style: ShapeStyle
  collapsed: string[]
  docked: boolean
}

function readPrefs(): Prefs {
  const fallback: Prefs = { style: "sketch", collapsed: [], docked: false }
  try {
    const parsed = JSON.parse(localStorage.getItem(PREFS_KEY) ?? "null") as Partial<Prefs> | null
    return {
      style: parsed?.style === "clean" ? "clean" : "sketch",
      collapsed: Array.isArray(parsed?.collapsed) ? parsed.collapsed.filter((c) => typeof c === "string") : [],
      docked: parsed?.docked === true,
    }
  } catch {
    return fallback
  }
}

function writePrefs(prefs: Prefs) {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(prefs))
  } catch {
    // Storage unavailable: the panel starts from its defaults next time.
  }
}

// ---------------------------------------------------------------------------
// Elements

/** A shape's elements, converted from its skeleton. Built fresh for every
 * insert, after the canvas has loaded its fonts, so labels are measured in
 * the font they are drawn in. */
function buildShape(shape: BuiltinShape, style: ShapeStyle): ExcalidrawElement[] {
  return convertToExcalidrawElements(shape.build(style), { regenerateIds: true })
}

/** Previews are converted once per shape and style and kept: the panel
 * re-renders on every keystroke in the search box. */
const previewCache = new Map<string, ExcalidrawElement[]>()
function previewElements(shape: BuiltinShape, style: ShapeStyle): ExcalidrawElement[] {
  const key = `${shape.key}:${shape.styled ? style : "-"}`
  let elements = previewCache.get(key)
  if (!elements) {
    elements = buildShape(shape, style)
    previewCache.set(key, elements)
  }
  return elements
}

function insertElements(api: ExcalidrawImperativeAPI, elements: readonly ExcalidrawElement[]) {
  const placed = centeredAt(withFreshIds(elements), viewportCenter(api.getAppState()))
  // Selected the way a click on it would select it: the outermost group, and
  // every element except labels, which Excalidraw selects through their
  // container.
  const groups = new Set(placed.map((el) => el.groupIds.at(-1)).filter((g): g is string => !!g))
  const selected = placed.filter((el) => !(el as { containerId?: string | null }).containerId)
  api.updateScene({
    elements: [...api.getSceneElementsIncludingDeleted(), ...placed],
    appState: {
      selectedElementIds: Object.fromEntries(selected.map((el) => [el.id, true])),
      selectedGroupIds: Object.fromEntries([...groups].map((g) => [g, true])),
      editingGroupId: null,
    } as never,
    captureUpdate: CaptureUpdateAction.IMMEDIATELY,
  })
  // On a narrow screen the panel covers the canvas; get out of the way of
  // the shape that was just put there.
  if (window.matchMedia("(max-width: 767px)").matches) api.toggleSidebar({ name: SHAPES_SIDEBAR, force: false })
}

function dragElements(event: React.DragEvent, elements: readonly ExcalidrawElement[]) {
  const item: LibraryItem = {
    id: randomId(),
    status: "unpublished",
    created: Date.now(),
    elements: withFreshIds(elements) as LibraryItem["elements"],
  }
  event.dataTransfer.setData(MIME_TYPES.excalidrawlib, serializeLibraryAsJSON([item]))
  event.dataTransfer.effectAllowed = "copy"
}

// ---------------------------------------------------------------------------
// Components

export function ShapesTrigger() {
  return (
    <Sidebar.Trigger name={SHAPES_SIDEBAR} icon={<Shapes className="size-4" />} title="Shapes">
      Shapes
    </Sidebar.Trigger>
  )
}

interface ShapesSidebarProps {
  api: ExcalidrawImperativeAPI | null
  /** The operator's own library, as Excalidraw last reported it. */
  personal: LibraryItems
  dark: boolean
}

export function ShapesSidebar({ api, personal, dark }: ShapesSidebarProps) {
  const [prefs, setPrefs] = useState(readPrefs)
  const [query, setQuery] = useState("")
  const update = (next: Partial<Prefs>) => {
    setPrefs((prev) => {
      const merged = { ...prev, ...next }
      writePrefs(merged)
      return merged
    })
  }

  const searching = query.trim() !== ""
  const sections = useMemo(
    () =>
      SHAPE_CATEGORIES.map((category) => ({
        ...category,
        shapes: BUILTIN_SHAPES.filter((s) => s.category === category.id && matchesShape(s, query)),
      })).filter((section) => section.shapes.length > 0),
    [query],
  )
  const mine = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    return personal.filter((item) => {
      const name = (item.name ?? "").toLowerCase()
      return words.every((w) => name.includes(w))
    })
  }, [personal, query])

  const nothing = sections.length === 0 && mine.length === 0

  return (
    <Sidebar
      name={SHAPES_SIDEBAR}
      docked={prefs.docked}
      onDock={(docked) => update({ docked })}
      className="wiki-shapes-sidebar"
    >
      <Sidebar.Header>
        <span className="text-sm font-semibold">Shapes</span>
      </Sidebar.Header>
      <div className="flex min-h-0 flex-1 flex-col gap-3 px-3 pb-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search shapes…"
            aria-label="Search shapes"
            className="h-8 w-full rounded-md border bg-background pl-8 pr-8 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
          {query && (
            <button
              type="button"
              onClick={() => setQuery("")}
              aria-label="Clear search"
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="size-4" />
            </button>
          )}
        </div>

        <div role="radiogroup" aria-label="Style" className="grid grid-cols-2 rounded-md border p-0.5 text-xs">
          {(["sketch", "clean"] as const).map((style) => (
            <button
              key={style}
              type="button"
              role="radio"
              aria-checked={prefs.style === style}
              onClick={() => update({ style })}
              className={cn(
                "rounded px-2 py-1 font-medium transition-colors",
                prefs.style === style ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:text-foreground",
              )}
              title={style === "sketch" ? "Hand-drawn, for working diagrams" : "Straight lines, for reports"}
            >
              {style === "sketch" ? "Sketch" : "Clean"}
            </button>
          ))}
        </div>

        <div className="-mx-3 min-h-0 flex-1 overflow-y-auto px-3">
          {nothing && <p className="py-8 text-center text-sm text-muted-foreground">No shapes match “{query}”.</p>}

          {sections.map((section) => {
            const open = searching || !prefs.collapsed.includes(section.id)
            return (
              <Section
                key={section.id}
                label={section.label}
                count={section.shapes.length}
                open={open}
                onToggle={() =>
                  update({
                    collapsed: open
                      ? [...prefs.collapsed, section.id]
                      : prefs.collapsed.filter((id) => id !== section.id),
                  })
                }
              >
                <div className={cn("grid gap-2", section.shapes[0].tile === "small" ? "grid-cols-4" : "grid-cols-2")}>
                  {section.shapes.map((shape) => (
                    <ShapeTile
                      key={shape.key}
                      name={shape.name}
                      small={shape.tile === "small"}
                      preview={previewElements(shape, prefs.style)}
                      dark={dark}
                      onInsert={() => api && insertElements(api, buildShape(shape, prefs.style))}
                      onDrag={(e) => dragElements(e, buildShape(shape, prefs.style))}
                    />
                  ))}
                </div>
              </Section>
            )
          })}

          {mine.length > 0 && (
            <Section
              label="My shapes"
              count={mine.length}
              open={searching || !prefs.collapsed.includes("mine")}
              onToggle={() =>
                update({
                  collapsed: prefs.collapsed.includes("mine")
                    ? prefs.collapsed.filter((id) => id !== "mine")
                    : [...prefs.collapsed, "mine"],
                })
              }
            >
              <div className="grid grid-cols-2 gap-2">
                {mine.map((item) => (
                  <ShapeTile
                    key={item.id}
                    name={item.name || "Untitled shape"}
                    preview={item.elements}
                    dark={dark}
                    onInsert={() => api && insertElements(api, item.elements)}
                    onDrag={(e) => dragElements(e, item.elements)}
                  />
                ))}
              </div>
            </Section>
          )}

          {!searching && personal.length === 0 && (
            <p className="mt-4 text-xs text-muted-foreground">
              Select shapes on the canvas and use “Add to library” to keep your own here.
            </p>
          )}
        </div>
      </div>
    </Sidebar>
  )
}

function Section({
  label,
  count,
  open,
  onToggle,
  children,
}: {
  label: string
  count: number
  open: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <section className="mb-3">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-center gap-1 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground hover:text-foreground"
      >
        <ChevronDown className={cn("size-3.5 transition-transform", !open && "-rotate-90")} />
        <span className="flex-1 text-left">{label}</span>
        <span className="font-normal tabular-nums">{count}</span>
      </button>
      {open && children}
    </section>
  )
}

function ShapeTile({
  name,
  preview,
  dark,
  small = false,
  onInsert,
  onDrag,
}: {
  name: string
  preview: readonly ExcalidrawElement[]
  dark: boolean
  small?: boolean
  onInsert: () => void
  onDrag: (event: React.DragEvent) => void
}) {
  return (
    <button
      type="button"
      draggable
      onClick={onInsert}
      onDragStart={onDrag}
      title={`${name}: click to add, or drag onto the canvas`}
      className="group flex min-w-0 flex-col items-stretch gap-1 rounded-md border border-transparent p-1 text-left transition-colors hover:border-border hover:bg-accent/60 focus-visible:border-ring focus-visible:outline-none"
    >
      <div className={cn("flex items-center justify-center rounded bg-muted/40", small ? "h-12" : "h-16")}>
        <Preview elements={preview} dark={dark} />
      </div>
      <span className={cn("truncate text-xs", small && "text-center")}>{name}</span>
    </button>
  )
}

/** A shape drawn by Excalidraw's own SVG export, so the preview is exactly
 * what gets inserted. Fonts are not inlined: the canvas has already loaded
 * them into the page, and inlining would subset a font for every tile. */
function Preview({ elements, dark }: { elements: readonly ExcalidrawElement[]; dark: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    let cancelled = false
    exportToSvg({
      elements,
      appState: { exportBackground: false, exportWithDarkMode: dark },
      files: null,
      exportPadding: 4,
      skipInliningFonts: true,
    })
      .then((svg: SVGSVGElement) => {
        if (cancelled || !ref.current) return
        svg.removeAttribute("width")
        svg.removeAttribute("height")
        svg.setAttribute("preserveAspectRatio", "xMidYMid meet")
        svg.style.width = "100%"
        svg.style.height = "100%"
        ref.current.replaceChildren(svg)
      })
      .catch(() => {
        // A preview that cannot be drawn leaves an empty tile; the name and
        // the insert still work.
      })
    return () => {
      cancelled = true
    }
  }, [elements, dark])
  return <div ref={ref} aria-hidden className="h-full w-full p-1" />
}

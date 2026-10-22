// The quick shape picker: "/" on the canvas opens it, typing narrows it, the
// arrow keys move through it and Enter puts the shape where the pointer is.
// The same shapes as the Shapes panel (drawing-shape-entries.ts), for when
// the hand is on the keyboard; the last one picked is where it opens next.
//
// "/" is free in Excalidraw: its own palette is Ctrl/⌘+/, the properties
// panel Alt+/, help Shift+/ (?).

import { useEffect, useMemo, useRef, useState } from "react"
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import type { LibraryItems } from "@excalidraw/excalidraw/types"
import { SearchIcon } from "lucide-react"

import { DialogOverlay } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"
import { BUILTIN_SHAPES, SHAPE_CATEGORIES, matchesShape, type BuiltinShape, type RefKind } from "./drawing-catalogue"
import {
  RECORD_TILES,
  buildShape,
  matchesRecord,
  previewElements,
  recordPreview,
  useShapePrefs,
} from "./drawing-shape-entries"
import { Preview } from "./drawing-shapes-panel"

export type QuickChoice =
  | { kind: "elements"; elements: readonly ExcalidrawElement[] }
  | { kind: "record"; record: RefKind }

interface Item {
  /** Stable: a built-in's key, record:<kind>, mine:<library id>. */
  key: string
  name: string
  section: string
  preview: readonly ExcalidrawElement[]
  choose: () => QuickChoice
}

interface QuickShapesProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The operator's own library, as the canvas knows it. */
  personal: LibraryItems
  dark: boolean
  /** Record tiles are offered only where records can be picked. */
  records: boolean
  onChoose: (choice: QuickChoice) => void
}

export function QuickShapes({ open, onOpenChange, ...rest }: QuickShapesProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogOverlay />
        <DialogPrimitive.Popup
          aria-label="Add a shape"
          className="fixed top-[12%] left-1/2 z-50 w-[calc(100%-2rem)] max-w-2xl -translate-x-1/2 rounded-xl bg-popover shadow-xl ring-1 ring-foreground/10 outline-none duration-100 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0"
        >
          {/* Mounted per opening, so each starts from an empty search. */}
          {open && <QuickShapesBody {...rest} onClose={() => onOpenChange(false)} />}
        </DialogPrimitive.Popup>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

function QuickShapesBody({
  personal,
  dark,
  records,
  onChoose,
  onClose,
}: Omit<QuickShapesProps, "open" | "onOpenChange"> & { onClose: () => void }) {
  const [prefs, update] = useShapePrefs()
  const [query, setQuery] = useState("")
  const style = prefs.style

  const items = useMemo(() => {
    const out: Item[] = []
    if (records) {
      for (const tile of RECORD_TILES) {
        if (!matchesRecord(tile, query)) continue
        out.push({
          key: `record:${tile.kind}`,
          name: tile.name,
          section: "Linked records",
          preview: recordPreview(tile, style),
          choose: () => ({ kind: "record", record: tile.kind }),
        })
      }
    }
    for (const category of SHAPE_CATEGORIES) {
      for (const shape of BUILTIN_SHAPES) {
        if (shape.category !== category.id || !matchesShape(shape, query)) continue
        out.push(shapeItem(shape, category.label, style))
      }
    }
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    for (const item of personal) {
      const name = item.name || "Untitled shape"
      if (!words.every((w) => name.toLowerCase().includes(w))) continue
      out.push({
        key: `mine:${item.id}`,
        name,
        section: "My shapes",
        preview: item.elements,
        choose: () => ({ kind: "elements", elements: item.elements }),
      })
    }
    return out
  }, [records, query, style, personal])

  // The last shape picked, while nothing is typed; else the first match.
  const [active, setActive] = useState(() => Math.max(0, items.findIndex((i) => i.key === prefs.last)))
  const [lastQuery, setLastQuery] = useState(query)
  if (lastQuery !== query) {
    setLastQuery(query)
    setActive(0)
  }
  const current = Math.min(active, Math.max(0, items.length - 1))

  // Runs of items with one section label, in order.
  const sections = useMemo(() => {
    const out: { label: string; from: number; to: number }[] = []
    items.forEach((item, i) => {
      const last = out[out.length - 1]
      if (last && last.label === item.section) last.to = i + 1
      else out.push({ label: item.section, from: i, to: i + 1 })
    })
    return out
  }, [items])

  const tiles = useRef<(HTMLButtonElement | null)[]>([])
  useEffect(() => {
    tiles.current[current]?.scrollIntoView({ block: "nearest" })
  }, [current])

  const choose = (item: Item | undefined) => {
    if (!item) return
    update({ last: item.key })
    onClose()
    onChoose(item.choose())
  }

  /** The tile in the row above or below the active one, nearest across. */
  const vertical = (dir: 1 | -1): number => {
    const from = tiles.current[current]?.getBoundingClientRect()
    if (!from) return current
    const cx = from.left + from.width / 2
    let best = current
    let bestScore = Infinity
    tiles.current.forEach((el, i) => {
      if (!el || i === current) return
      const r = el.getBoundingClientRect()
      const dy = (r.top - from.top) * dir
      if (dy <= 1) return
      // Rows first, then the nearest across.
      const score = dy * 10_000 + Math.abs(r.left + r.width / 2 - cx)
      if (score < bestScore) {
        bestScore = score
        best = i
      }
    })
    return best
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    const last = items.length - 1
    switch (e.key) {
      case "ArrowRight":
        setActive(Math.min(last, current + 1))
        break
      case "ArrowLeft":
        setActive(Math.max(0, current - 1))
        break
      case "ArrowDown":
        setActive(vertical(1))
        break
      case "ArrowUp":
        setActive(vertical(-1))
        break
      case "Home":
        setActive(0)
        break
      case "End":
        setActive(last)
        break
      case "Enter":
        choose(items[current])
        break
      case "Tab":
        update({ style: style === "sketch" ? "clean" : "sketch" })
        break
      default:
        return
    }
    e.preventDefault()
  }

  return (
    <div className="flex flex-col" onKeyDown={onKeyDown}>
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <SearchIcon className="size-4 shrink-0 text-muted-foreground" />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search shapes…"
          aria-label="Search shapes"
          aria-activedescendant={items[current] ? `quick-shape-${current}` : undefined}
          autoFocus
          maxLength={100}
          className="h-8 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        />
        <button
          type="button"
          onClick={() => update({ style: style === "sketch" ? "clean" : "sketch" })}
          className="shrink-0 rounded-md border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
          title="Switch style (Tab)"
        >
          {style === "sketch" ? "Sketch" : "Clean"}
        </button>
      </div>

      <div role="listbox" aria-label="Shapes" className="max-h-[55vh] min-h-32 overflow-y-auto px-3 pb-3">
        {items.length === 0 && (
          <p className="py-8 text-center text-sm text-muted-foreground">No shapes match “{query}”.</p>
        )}
        {sections.map((sec) => (
          <section key={sec.label}>
            <div className="pt-3 pb-1.5 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
              {sec.label}
            </div>
            <SectionGrid items={items} from={sec.from} to={sec.to} {...{ current, tiles, dark, choose, setActive }} />
          </section>
        ))}
      </div>

      <div className="flex items-center gap-3 border-t px-4 py-2 text-xs text-muted-foreground">
        <span>
          <Kbd>←</Kbd> <Kbd>→</Kbd> <Kbd>↑</Kbd> <Kbd>↓</Kbd> to move
        </span>
        <span>
          <Kbd>Enter</Kbd> to add
        </span>
        <span>
          <Kbd>Tab</Kbd> style
        </span>
        <span className="ml-auto">
          <Kbd>Esc</Kbd> to close
        </span>
      </div>
    </div>
  )
}

/** One section's tiles: the items from `from` up to `to`. */
function SectionGrid({
  items,
  from,
  to,
  current,
  tiles,
  dark,
  choose,
  setActive,
}: {
  items: Item[]
  from: number
  to: number
  current: number
  tiles: React.RefObject<(HTMLButtonElement | null)[]>
  dark: boolean
  choose: (item: Item) => void
  setActive: (i: number) => void
}) {
  return (
    <div className="grid grid-cols-4 gap-1.5">
      {items.slice(from, to).map((item, k) => {
        const i = from + k
        return (
          <button
            key={item.key}
            id={`quick-shape-${i}`}
            ref={(el) => {
              tiles.current[i] = el
            }}
            type="button"
            role="option"
            aria-selected={i === current}
            tabIndex={-1}
            onClick={() => choose(item)}
            onMouseMove={() => i !== current && setActive(i)}
            className={cn(
              "flex min-w-0 flex-col items-stretch gap-1 rounded-md border p-1 text-left transition-colors",
              i === current ? "border-ring bg-accent" : "border-transparent hover:bg-accent/60",
            )}
          >
            <div className="flex h-14 items-center justify-center rounded bg-muted/40">
              <Preview elements={item.preview} dark={dark} />
            </div>
            <span className="truncate text-xs">{item.name}</span>
          </button>
        )
      })}
    </div>
  )
}

function shapeItem(shape: BuiltinShape, section: string, style: "sketch" | "clean"): Item {
  return {
    key: shape.key,
    name: shape.name,
    section,
    preview: previewElements(shape, style),
    choose: () => ({ kind: "elements", elements: buildShape(shape, style) }),
  }
}

function Kbd({ children }: { children: React.ReactNode }) {
  return <kbd className="rounded border px-1 font-sans">{children}</kbd>
}

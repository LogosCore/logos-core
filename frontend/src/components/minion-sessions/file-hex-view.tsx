import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { CopyIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"
import {
  BYTES_PER_ROW,
  asciiChar,
  asciiString,
  blocksFor,
  formatOffset,
  hexByte,
  hexString,
  hexPlaceOf,
  inspect,
  offsetDigits,
  parseOffset,
  type HexPlace,
} from "./hex"
import { readRange, type ByteBlocks } from "./use-byte-blocks"
import { usePaneRow } from "./pane-text"

// A kept file's bytes, read-only: offset, 16 bytes in hex, the same as text.
// The view scrolls itself rather than the page laying out every row — a
// 2 GiB file is 134 million of them, past what a browser can make tall —
// so the wheel, the keys and its own scrollbar move a row index, and only
// the rows in sight are drawn, from blocks read as they come in sight
// (use-byte-blocks.ts). Where it is and what is selected are its parent's,
// kept while another tab shows.

/** The most a selection copies. */
const MAX_COPY = 1 << 20

export function FileHexView({
  url,
  size,
  bytes,
  place,
  onPlace,
  toolbar,
}: {
  url: string
  size: number
  bytes: ByteBlocks
  place: HexPlace
  onPlace: (update: (p: HexPlace) => HexPlace) => void
  /** Renders the tool row: the tabs, with these tools at its end. */
  toolbar: (tools: ReactNode) => ReactNode
}) {
  // 20px at 12px text: as tall as the pane's text makes it.
  const ROW_H = usePaneRow(20)
  const totalRows = Math.max(1, Math.ceil(size / BYTES_PER_ROW))
  const digits = offsetDigits(size)
  const bodyRef = useRef<HTMLDivElement>(null)
  const [viewH, setViewH] = useState(0)
  const visible = Math.max(1, Math.floor(viewH / ROW_H))
  const maxTop = Math.max(0, totalRows - visible)
  const top = Math.min(Math.max(0, place.top), maxTop)
  const sel = place.sel
  const lo = sel ? Math.min(sel.anchor, sel.caret) : -1
  const hi = sel ? Math.max(sel.anchor, sel.caret) : -1
  const [hover, setHover] = useState<number | null>(null)
  const [goto, setGoto] = useState("")
  const [gotoBad, setGotoBad] = useState(false)
  const dragging = useRef(false)

  // Measured before paint: the rows in sight come from it.
  useLayoutEffect(() => {
    const el = bodyRef.current
    if (!el) return
    setViewH(el.clientHeight)
    const observer = new ResizeObserver(() => setViewH(el.clientHeight))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const { want } = bytes
  useEffect(() => want(blocksFor(top, top + visible + 1, size)), [want, top, visible, size])

  // The wheel moves rows; not passive, so the page does not scroll too.
  useEffect(() => {
    const el = bodyRef.current
    if (!el) return
    let rest = 0
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      rest += e.deltaMode === 1 ? e.deltaY * ROW_H * 3 : e.deltaMode === 2 ? e.deltaY * el.clientHeight : e.deltaY
      const rows = Math.trunc(rest / ROW_H)
      if (rows === 0) return
      rest -= rows * ROW_H
      onPlace((p) => ({ ...p, top: Math.min(Math.max(0, Math.min(p.top, maxTop) + rows), maxTop) }))
    }
    el.addEventListener("wheel", onWheel, { passive: false })
    return () => el.removeEventListener("wheel", onWheel)
  }, [onPlace, maxTop, ROW_H])

  /** Moves the caret, the anchor with it unless extending, and the rows to keep it in sight. */
  function moveTo(caret: number, extend: boolean) {
    const c = Math.min(Math.max(0, caret), Math.max(0, size - 1))
    const row = Math.floor(c / BYTES_PER_ROW)
    onPlace((p) => {
      const t = Math.min(Math.max(0, p.top), maxTop)
      return {
        top: row < t ? row : row >= t + visible ? row - visible + 1 : t,
        sel: { anchor: extend && p.sel ? p.sel.anchor : c, caret: c },
      }
    })
  }

  async function copy(as: "hex" | "text") {
    if (!sel) return
    if (hi - lo + 1 > MAX_COPY) {
      toast.error("Select at most 1 MiB to copy it.")
      return
    }
    try {
      const data = await readRange(url, lo, hi + 1)
      await copyToClipboard(as === "hex" ? hexString(data) : asciiString(data), as === "hex" ? "bytes as hex" : "bytes as text")
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Couldn't read the bytes.")
    }
  }

  function onKeyDown(e: React.KeyboardEvent) {
    const caret = sel?.caret ?? top * BYTES_PER_ROW
    const ext = e.shiftKey
    const page = visible * BYTES_PER_ROW
    const keys: Record<string, () => void> = {
      ArrowLeft: () => moveTo(caret - 1, ext),
      ArrowRight: () => moveTo(caret + 1, ext),
      ArrowUp: () => moveTo(caret - BYTES_PER_ROW, ext),
      ArrowDown: () => moveTo(caret + BYTES_PER_ROW, ext),
      PageUp: () => moveTo(caret - page, ext),
      PageDown: () => moveTo(caret + page, ext),
      Home: () => moveTo(e.ctrlKey || e.metaKey ? 0 : caret - (caret % BYTES_PER_ROW), ext),
      End: () => moveTo(e.ctrlKey || e.metaKey ? size - 1 : caret - (caret % BYTES_PER_ROW) + BYTES_PER_ROW - 1, ext),
    }
    if (keys[e.key]) {
      e.preventDefault()
      keys[e.key]()
    } else if ((e.ctrlKey || e.metaKey) && e.key === "c" && sel) {
      e.preventDefault()
      void copy("hex")
    } else if ((e.ctrlKey || e.metaKey) && e.key === "a") {
      e.preventDefault()
      onPlace((p) => ({ ...p, sel: { anchor: 0, caret: size - 1 } }))
    }
  }

  function offsetAt(target: EventTarget): number | null {
    const el = (target as HTMLElement).closest<HTMLElement>("[data-off]")
    return el ? Number(el.dataset.off) : null
  }

  function goTo() {
    const off = parseOffset(goto)
    if (off === null || off >= size) {
      setGotoBad(true)
      return
    }
    setGotoBad(false)
    onPlace(() => hexPlaceOf(off, 1))
    bodyRef.current?.focus()
  }

  // The bytes from the caret, for the inspector: as many of 8 as are read.
  const caretBytes: number[] = []
  if (sel) {
    for (let o = sel.caret; o < Math.min(sel.caret + 8, size); o++) {
      const b = bytes.byteAt(o)
      if (b === undefined) break
      caretBytes.push(b)
    }
  }

  const rows: ReactNode[] = []
  for (let i = 0; i < visible + 1; i++) {
    const row = top + i
    const base = row * BYTES_PER_ROW
    if (base >= size) break
    const hexCells: ReactNode[] = []
    const textCells: ReactNode[] = []
    for (let j = 0; j < BYTES_PER_ROW; j++) {
      const off = base + j
      const b = off < size ? bytes.byteAt(off) : undefined
      const inFile = off < size
      const cell = cn(
        inFile && off >= lo && off <= hi && "bg-sky-500/25",
        inFile && off === sel?.caret && "outline outline-1 -outline-offset-1 outline-sky-500",
        inFile && off === hover && !(off >= lo && off <= hi) && "bg-muted",
        b === 0 && "text-muted-foreground/50",
        b === undefined && "text-muted-foreground/40",
      )
      hexCells.push(
        <span key={j} data-off={inFile ? off : undefined} className={cn("inline-block w-[2ch] text-center", j > 0 && (j === 8 ? "ml-[2ch]" : "ml-[1ch]"), cell)}>
          {inFile ? (b === undefined ? "··" : hexByte(b)) : ""}
        </span>,
      )
      textCells.push(
        <span key={j} data-off={inFile ? off : undefined} className={cn("inline-block w-[1ch] text-center", cell)}>
          {inFile ? (b === undefined ? "·" : asciiChar(b)) : ""}
        </span>,
      )
    }
    rows.push(
      <div key={row} className="flex items-center whitespace-pre" style={{ height: ROW_H }}>
        <span className="w-[calc(var(--d)*1ch)] shrink-0 text-muted-foreground select-none" style={{ "--d": digits } as React.CSSProperties}>
          {formatOffset(base, digits)}
        </span>
        <span className="ml-[3ch] shrink-0">{hexCells}</span>
        <span className="ml-[3ch] shrink-0">{textCells}</span>
      </div>,
    )
  }

  const selLen = sel ? hi - lo + 1 : 0
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {toolbar(
        <>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              goTo()
            }}
          >
            <Input
              value={goto}
              onChange={(e) => {
                setGoto(e.target.value)
                setGotoBad(false)
              }}
              placeholder="Go to offset"
              title="An offset in hex (0x1F40, 1F40h) or decimal (8000); Enter goes"
              aria-label="Go to offset"
              aria-invalid={gotoBad || undefined}
              className="h-6 w-32 px-2 font-mono text-xs"
            />
          </form>
          <Button variant="ghost" size="xs" disabled={!sel} title="Copy the selected bytes as hex (Ctrl+C)" onClick={() => void copy("hex")}>
            <CopyIcon />
            Hex
          </Button>
          <Button variant="ghost" size="xs" disabled={!sel} title="Copy the selected bytes as text" onClick={() => void copy("text")}>
            <CopyIcon />
            Text
          </Button>
        </>,
      )}
      <div className="flex min-h-0 flex-1">
        <div
          ref={bodyRef}
          tabIndex={0}
          role="grid"
          aria-label="The file's bytes"
          aria-readonly
          className="min-h-0 min-w-0 flex-1 cursor-text overflow-x-auto overflow-y-hidden px-3 py-1 font-mono text-xs select-none focus-visible:outline-none"
          onKeyDown={onKeyDown}
          onMouseDown={(e) => {
            const off = offsetAt(e.target)
            if (off === null || e.button !== 0) return
            dragging.current = true
            onPlace((p) => ({ ...p, sel: { anchor: e.shiftKey && p.sel ? p.sel.anchor : off, caret: off } }))
          }}
          onMouseMove={(e) => {
            const off = offsetAt(e.target)
            setHover(off)
            if (dragging.current && off !== null && e.buttons === 1) {
              onPlace((p) => (p.sel && p.sel.caret !== off ? { ...p, sel: { anchor: p.sel.anchor, caret: off } } : p))
            }
          }}
          onMouseUp={() => (dragging.current = false)}
          onMouseLeave={() => {
            dragging.current = false
            setHover(null)
          }}
        >
          {rows}
        </div>
        <Scrollbar top={top} maxTop={maxTop} visible={visible} total={totalRows} onTop={(t) => onPlace((p) => ({ ...p, top: t }))} />
      </div>
      <div className="flex min-h-7 flex-wrap items-center gap-x-4 gap-y-0.5 border-t px-3 py-1 font-mono text-xs text-muted-foreground tabular-nums">
        {bytes.error ? (
          <span className="text-red-600 dark:text-red-400">{bytes.error}</span>
        ) : sel ? (
          <>
            <span>
              <span className="text-foreground">0x{formatOffset(sel.caret, digits)}</span> ({sel.caret.toLocaleString("en-US")})
            </span>
            {selLen > 1 && (
              <span>
                {selLen.toLocaleString("en-US")} bytes: 0x{formatOffset(lo, digits)}–0x{formatOffset(hi, digits)}
              </span>
            )}
            {inspect(caretBytes).map((v) => (
              <span key={v.label}>
                {v.label} <span className="text-foreground">{v.value}</span>
              </span>
            ))}
          </>
        ) : (
          <span>{size.toLocaleString("en-US")} bytes. Click a byte to select it; arrows move, Shift extends.</span>
        )}
      </div>
    </div>
  )
}

// The rows' scrollbar: a thumb as long as the rows in sight are of all of
// them (never under 24 px), dragged; a click on the track pages.
function Scrollbar({ top, maxTop, visible, total, onTop }: { top: number; maxTop: number; visible: number; total: number; onTop: (top: number) => void }) {
  const trackRef = useRef<HTMLDivElement>(null)
  const [trackH, setTrackH] = useState(0)
  const drag = useRef<{ y: number; top: number } | null>(null)
  useLayoutEffect(() => {
    const el = trackRef.current
    if (!el) return
    setTrackH(el.clientHeight)
    const observer = new ResizeObserver(() => setTrackH(el.clientHeight))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  if (maxTop === 0) return null
  const thumbH = Math.max(24, (trackH * visible) / total)
  const room = Math.max(1, trackH - thumbH)
  const thumbTop = (top / maxTop) * room
  const clamp = (t: number) => Math.min(Math.max(0, Math.round(t)), maxTop)
  return (
    <div
      ref={trackRef}
      className="relative w-3 shrink-0 border-l bg-muted/40"
      onPointerDown={(e) => {
        if (e.target !== e.currentTarget) return
        const y = e.clientY - e.currentTarget.getBoundingClientRect().top
        onTop(clamp(y < thumbTop ? top - visible : top + visible))
      }}
    >
      <div
        className="absolute inset-x-0.5 cursor-grab rounded-full bg-muted-foreground/40 hover:bg-muted-foreground/60 active:cursor-grabbing"
        style={{ top: thumbTop, height: thumbH }}
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId)
          drag.current = { y: e.clientY, top }
        }}
        onPointerMove={(e) => {
          if (!drag.current) return
          onTop(clamp(drag.current.top + ((e.clientY - drag.current.y) * maxTop) / room))
        }}
        onPointerUp={() => (drag.current = null)}
      />
    </div>
  )
}

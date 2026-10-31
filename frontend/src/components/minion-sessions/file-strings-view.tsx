import { useEffect, useRef, useState, type ReactNode } from "react"
import { useVirtualizer } from "@tanstack/react-virtual"
import { BinaryIcon, CopyIcon } from "lucide-react"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useMinionFileStrings, type MinionFileStringsFilter } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { formatOffset, offsetDigits } from "./hex"

// A kept file's printable strings, as strings(1) finds them: read by core a
// page at a time from where the last stopped, searched and filtered there,
// shown as they come, more read as the list nears its end. A string opens
// in the hex view, selected.

const ROW_H = 24
const MIN_LENGTHS = [3, 4, 5, 6, 8, 10, 16, 32]
const ENCODINGS: { value: MinionFileStringsFilter["encoding"]; label: string }[] = [
  { value: "", label: "ASCII and UTF-16" },
  { value: "ascii", label: "ASCII" },
  { value: "utf16le", label: "UTF-16LE" },
]

export function FileStringsView({
  fileId,
  size,
  filter,
  onFilter,
  onShowInHex,
  toolbar,
}: {
  fileId: string
  size: number
  filter: MinionFileStringsFilter
  onFilter: (filter: MinionFileStringsFilter) => void
  onShowInHex: (offset: number, length: number) => void
  toolbar: (tools: ReactNode) => ReactNode
}) {
  const q = useMinionFileStrings(fileId, filter)
  const pages = q.data?.pages ?? []
  const items = pages.flatMap((p) => p.minionFileStrings.strings)
  const readTo = pages.length > 0 ? pages[pages.length - 1].minionFileStrings.next : 0
  const done = pages.length > 0 && pages[pages.length - 1].minionFileStrings.done
  const digits = offsetDigits(size)
  const [picked, setPicked] = useState<number | null>(null)

  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_H,
    overscan: 20,
  })
  const virtualItems = virtualizer.getVirtualItems()
  const lastIndex = virtualItems.length > 0 ? virtualItems[virtualItems.length - 1].index : -1

  // More as the end nears: page after page while one comes back short (a
  // search that matches little), until the list fills or the file is read.
  const { hasNextPage, isFetchingNextPage, fetchNextPage, isError } = q
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isError && lastIndex >= items.length - 50) void fetchNextPage()
  }, [hasNextPage, isFetchingNextPage, isError, fetchNextPage, lastIndex, items.length, pages.length])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {toolbar(
        <>
          <SearchInput
            value={filter.search}
            onValueChange={(search) => search !== filter.search && onFilter({ ...filter, search })}
            placeholder="Search"
            title="Strings that hold it, in any case: searched by core, through the whole file"
            className="relative w-44"
            inputClassName={cn("h-6 pl-8 text-xs", filter.search && "pr-7")}
          />
          <Select value={String(filter.minLength)} onValueChange={(v) => onFilter({ ...filter, minLength: Number(v) })}>
            <SelectTrigger size="sm" className="h-6 text-xs" aria-label="Shortest string" title="The fewest characters a string has">
              <SelectValue>{(v: string) => `${v}+ chars`}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {MIN_LENGTHS.map((n) => (
                <SelectItem key={n} value={String(n)}>
                  {n}+ chars
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filter.encoding || "all"} onValueChange={(v) => onFilter({ ...filter, encoding: v === "all" ? "" : (v as MinionFileStringsFilter["encoding"]) })}>
            <SelectTrigger size="sm" className="h-6 text-xs" aria-label="Encoding">
              <SelectValue>{(v: string) => ENCODINGS.find((e) => (e.value || "all") === v)?.label}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {ENCODINGS.map((e) => (
                <SelectItem key={e.value || "all"} value={e.value || "all"}>
                  {e.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>,
      )}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto">
        {q.isError ? (
          <p className="p-4 text-sm text-red-600 dark:text-red-400">{q.error instanceof Error ? q.error.message : "Couldn't read the strings."}</p>
        ) : items.length === 0 && done ? (
          <p className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">
            {filter.search ? "No string holds that." : "No strings."}
          </p>
        ) : (
          <div className="relative w-full font-mono text-xs" style={{ height: virtualizer.getTotalSize() }}>
            {virtualItems.map((v) => {
              const s = items[v.index]
              return (
                <ContextMenu key={v.key} onOpenChange={(open) => open && setPicked(v.index)}>
                  <ContextMenuTrigger
                    className={cn(
                      "absolute inset-x-0 flex cursor-default items-center gap-3 px-3 hover:bg-muted/60",
                      picked === v.index && "bg-muted",
                    )}
                    style={{ top: v.start, height: ROW_H }}
                    onClick={() => setPicked(v.index)}
                    onDoubleClick={() => onShowInHex(s.offset, s.length)}
                    title={s.text.length > 80 ? s.text : undefined}
                  >
                    <span className="shrink-0 text-muted-foreground tabular-nums">{formatOffset(s.offset, digits)}</span>
                    <span
                      className="w-9 shrink-0 text-[10px] tracking-wide text-muted-foreground uppercase"
                      title={s.encoding === "utf16le" ? "UTF-16LE: each character followed by a zero byte" : "ASCII: a byte a character"}
                    >
                      {s.encoding === "utf16le" ? "utf16" : "ascii"}
                    </span>
                    <span className="min-w-0 flex-1 truncate whitespace-pre">{s.text}</span>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem onClick={() => onShowInHex(s.offset, s.length)}>
                      <BinaryIcon />
                      Show in Hex
                    </ContextMenuItem>
                    <ContextMenuItem onClick={() => void copyToClipboard(s.text, "string")}>
                      <CopyIcon />
                      Copy string
                    </ContextMenuItem>
                    <ContextMenuItem onClick={() => void copyToClipboard(`0x${formatOffset(s.offset, digits)}`, "offset")}>
                      <CopyIcon />
                      Copy offset
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              )
            })}
          </div>
        )}
      </div>
      <div className="flex min-h-7 items-center gap-3 border-t px-3 py-1 text-xs text-muted-foreground tabular-nums">
        <span>
          {items.length.toLocaleString("en-US")} {items.length === 1 ? "string" : "strings"}
        </span>
        <span>
          {done ? "the whole file read" : `${formatBytes(readTo)} of ${formatBytes(size)} read${isFetchingNextPage || q.isLoading ? "…" : ""}`}
        </span>
        <span className="ml-auto">Double-click a string to show it in Hex; right-click for more.</span>
      </div>
    </div>
  )
}

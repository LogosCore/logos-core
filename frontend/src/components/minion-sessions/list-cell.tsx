import type { ReactNode } from "react"
import { CopyIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"

// A table cell for a list value (a host's addresses): one item shown, and a
// "+N" button that opens all of them. Where the viewer may change it, the
// menu also picks which item the row shows: the checked one. Each item has
// its own copy button; the last entry copies them all.

export function ListCell({
  items,
  shown,
  noun,
  onPick,
  detail,
  mono = true,
}: {
  items: readonly string[]
  /** The item the row shows. */
  shown: string | null
  /** Plural, for the menu: "addresses". */
  noun: string
  /** Picks the shown item; absent where the viewer may not. */
  onPick?: (item: string) => void
  /** More about an item, muted after it in the menu. */
  detail?: (item: string) => ReactNode
  mono?: boolean
}) {
  if (items.length === 0) return <span className="text-muted-foreground">—</span>
  const others = items.length - 1
  return (
    <div className="flex min-w-0 items-center gap-1">
      <span className={cn("truncate", mono && "font-mono text-xs")} title={shown ?? undefined}>
        {shown}
      </span>
      {others > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="xs"
                className="h-5 shrink-0 px-1 text-xs text-muted-foreground"
                aria-label={`All ${items.length} ${noun}`}
                title={`All ${items.length} ${noun}`}
              >
                +{others}
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="max-h-96 min-w-56 overflow-y-auto">
            {/* A label must sit in a group, or Base UI throws. */}
            <DropdownMenuGroup>
              <DropdownMenuLabel>
                {items.length} {noun}
              </DropdownMenuLabel>
              {onPick ? (
                <DropdownMenuRadioGroup value={shown} onValueChange={(v) => v !== shown && onPick(v as string)}>
                  {items.map((item) => (
                    <DropdownMenuRadioItem key={item} value={item}>
                      <span className={cn("truncate", mono && "font-mono text-xs")}>{item}</span>
                      {detail && <span className="truncate text-xs text-muted-foreground">{detail(item)}</span>}
                      <CopyItemButton item={item} />
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              ) : (
                items.map((item) => (
                  <DropdownMenuItem key={item} onClick={() => void copyToClipboard(item, item)}>
                    <span className={cn("truncate", mono && "font-mono text-xs", item === shown && "font-semibold")}>{item}</span>
                    {detail && <span className="truncate text-xs text-muted-foreground">{detail(item)}</span>}
                    <CopyItemButton item={item} />
                  </DropdownMenuItem>
                ))
              )}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void copyToClipboard(items.join("\n"), `${items.length} ${noun}`)}>
              <CopyIcon />
              Copy all {noun}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}

/**
 * Copies one item, at the end of its menu row. Its events stop here: the
 * click copies without picking the row's item or closing the menu.
 */
function CopyItemButton({ item }: { item: string }) {
  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation()
  return (
    <button
      type="button"
      aria-label={`Copy ${item}`}
      title={`Copy ${item}`}
      className="ml-auto shrink-0 cursor-pointer rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground"
      onPointerDown={stop}
      onPointerUp={stop}
      onMouseDown={stop}
      onMouseUp={stop}
      onKeyDown={stop}
      onClick={(e) => {
        e.stopPropagation()
        e.preventDefault()
        void copyToClipboard(item, item)
      }}
    >
      <CopyIcon className="size-3.5" />
    </button>
  )
}

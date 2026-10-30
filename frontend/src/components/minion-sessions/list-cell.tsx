import { CopyIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
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
// menu also picks which item the row shows; "Automatic" leaves it to the
// guess. Copy the shown item or all of them from the same menu.

const AUTO = "\u0000auto"

export function ListCell({
  items,
  shown,
  picked,
  autoItem,
  noun,
  onPick,
  mono = true,
}: {
  items: readonly string[]
  /** The item the row shows. */
  shown: string | null
  /** The item someone picked; null while the guess decides. */
  picked: string | null
  /** What "Automatic" would show. */
  autoItem: string | null
  /** Plural, for the menu: "addresses". */
  noun: string
  /** Picks the shown item (null: automatic); absent where the viewer may not. */
  onPick?: (item: string | null) => void
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
            <DropdownMenuLabel>
              {items.length} {noun}
            </DropdownMenuLabel>
            {onPick ? (
              <DropdownMenuRadioGroup
                value={picked ?? AUTO}
                onValueChange={(v) => onPick(v === AUTO ? null : (v as string))}
              >
                <DropdownMenuRadioItem value={AUTO}>
                  <span>Automatic</span>
                  {autoItem && <span className={cn("truncate text-muted-foreground", mono && "font-mono text-xs")}>{autoItem}</span>}
                </DropdownMenuRadioItem>
                {items.map((item) => (
                  <DropdownMenuRadioItem key={item} value={item}>
                    <span className={cn("truncate", mono && "font-mono text-xs", item === shown && "font-semibold")}>{item}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            ) : (
              items.map((item) => (
                <DropdownMenuItem key={item} onClick={() => void copyToClipboard(item, item)}>
                  <span className={cn("truncate", mono && "font-mono text-xs", item === shown && "font-semibold")}>{item}</span>
                </DropdownMenuItem>
              ))
            )}
            <DropdownMenuSeparator />
            {shown && (
              <DropdownMenuItem onClick={() => void copyToClipboard(shown, shown)}>
                <CopyIcon />
                Copy {shown}
              </DropdownMenuItem>
            )}
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

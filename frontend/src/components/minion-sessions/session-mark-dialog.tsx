import { useState } from "react"
import { CheckIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { errorMessage } from "@/components/modules/management/api-errors"
import { DocumentIconPicker } from "@/components/wiki/document-icon-picker"
import type { MinionSessionMarkInput } from "@/graphql/gql/graphql"
import { cn } from "@/lib/utils"
import {
  MARK_TONE_CLASSES,
  SESSION_MARK_COLORS,
  SESSION_MARK_HUES,
  SESSION_MARK_STRENGTHS,
  SESSION_MARK_STYLES,
  SESSION_MARK_STYLE_BY_ID,
  isMarkColor,
  markColorVars,
} from "./session-mark-colors"

// An operator's mark on sessions: a color that tints the row and an icon (or
// emoji) beside the number. Marks are the operator's own; nobody else sees
// them. One dialog marks one session or many alike.

export interface SessionMarkTarget {
  /** What is being marked, for the title: "Session #12", "4 sessions". */
  title: string
  /** The mark it starts from: the session's own, or none for many. */
  mark: MinionSessionMarkInput | null
  /** Many sessions: offers clearing their marks, whatever they are. */
  many?: boolean
  /** Saves the mark (null clears it); resolves once saved. */
  save: (mark: MinionSessionMarkInput | null) => Promise<void>
}

export function SessionMarkDialog({ target, onClose }: { target: SessionMarkTarget | null; onClose: () => void }) {
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {/* Keyed by the title: each opening starts from its target's mark. */}
        {target && <Editor key={target.title} target={target} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function Editor({ target, onClose }: { target: SessionMarkTarget; onClose: () => void }) {
  const [color, setColor] = useState(target.mark?.color ?? "")
  const [icon, setIcon] = useState(target.mark?.icon ?? "")
  const [emoji, setEmoji] = useState(target.mark?.emoji ?? "")
  const [pending, setPending] = useState(false)
  // Picks a color or a style, or none. A style brings its own icon; leaving
  // it takes that icon away too, unless the operator chose another since.
  const choose = (next: string) => {
    const was = SESSION_MARK_STYLE_BY_ID.get(color)
    if (was && icon === was.icon) setIcon("")
    const style = SESSION_MARK_STYLE_BY_ID.get(next)
    if (style) {
      setIcon(style.icon)
      setEmoji("")
    }
    setColor(next)
  }
  const [error, setError] = useState<string | null>(null)
  const submit = async (mark: MinionSessionMarkInput | null) => {
    setError(null)
    setPending(true)
    try {
      await target.save(mark && (mark.color || mark.icon || mark.emoji) ? mark : null)
      onClose()
    } catch (err) {
      setError(errorMessage(err, "Failed to save the mark"))
    } finally {
      setPending(false)
    }
  }
  const hadMark = target.many || !!(target.mark?.color || target.mark?.icon || target.mark?.emoji)
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        void submit({ color, icon, emoji })
      }}
    >
      <DialogHeader>
        <DialogTitle>Mark {target.title}</DialogTitle>
        <DialogDescription>A color or a style for the row, and an icon beside the number. Only you see your marks.</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium">Color or style</span>
          <Button type="button" variant="ghost" size="xs" disabled={!color} onClick={() => choose("")}>
            <XIcon />
            None
          </Button>
        </div>
        {/* A column per hue, soft to strong down; the styles a row below. */}
        <div role="radiogroup" aria-label="Color or style" className="grid grid-cols-12 gap-1">
          {SESSION_MARK_COLORS.map((c) => (
            <button
              key={c.id}
              type="button"
              role="radio"
              aria-checked={color === c.id}
              aria-label={c.label}
              title={c.label}
              onClick={() => choose(color === c.id ? "" : c.id)}
              style={{
                ...markColorVars(c.id),
                gridColumn: SESSION_MARK_HUES.indexOf(c.hue) + 1,
                gridRow: SESSION_MARK_STRENGTHS.indexOf(c.strength) + 1,
              }}
              className={cn(
                "flex aspect-square items-center justify-center rounded-md border border-foreground/10 bg-(color:--mark-row) outline-none hover:bg-(color:--mark-row-hover) focus-visible:ring-2 focus-visible:ring-ring dark:bg-(color:--mark-row-dark) dark:hover:bg-(color:--mark-row-dark-hover)",
                MARK_TONE_CLASSES,
                color === c.id && "ring-2 ring-foreground/70",
              )}
            >
              {color === c.id ? <CheckIcon className="size-3.5" /> : <span className="size-2 rounded-full bg-current" />}
            </button>
          ))}
          {SESSION_MARK_STYLES.map((st, i) => (
            <button
              key={st.id}
              type="button"
              role="radio"
              aria-checked={color === st.id}
              aria-label={st.label}
              title={st.label}
              onClick={() => choose(color === st.id ? "" : st.id)}
              style={{ ...st.swatch, gridColumn: i + 1, gridRow: SESSION_MARK_STRENGTHS.length + 1 }}
              className={cn(
                "flex aspect-square items-center justify-center rounded-md border border-foreground/10 outline-none hover:brightness-125 focus-visible:ring-2 focus-visible:ring-ring",
                color === st.id && "ring-2 ring-foreground/70",
              )}
            >
              <st.Icon className="size-3.5" />
            </button>
          ))}
        </div>
      </div>
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium">Icon</span>
        {/* The picker's icon takes the chosen color's tone. */}
        <span style={markColorVars(color)} className={cn("inline-flex", isMarkColor(color) && MARK_TONE_CLASSES)}>
          <DocumentIconPicker
            value={{ emoji, icon, color: "" }}
            onSelect={(v) => {
              setIcon(v.icon)
              setEmoji(v.emoji)
            }}
            allowAdaptive={false}
            hideColors
          />
        </span>
        {!icon && !emoji && <span className="text-xs text-muted-foreground">None: pick one</span>}
        {(icon || emoji) && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={() => {
              setIcon("")
              setEmoji("")
            }}
          >
            <XIcon />
            No icon
          </Button>
        )}
      </div>
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        {hadMark && (
          <Button type="button" variant="ghost" className="sm:mr-auto" disabled={pending} onClick={() => void submit(null)}>
            Clear mark
          </Button>
        )}
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? "Saving..." : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

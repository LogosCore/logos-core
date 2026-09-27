import { type ReactNode } from "react"
import { ChevronRightIcon, PlusIcon } from "lucide-react"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

interface CollapsibleSectionProps {
  title: string
  /**
   * Rendered next to the title whether or not the section is collapsible, so
   * a collapsed header still says how much it is hiding.
   */
  count: number
  /**
   * Trailing slot in the header row. Kept outside the disclosure trigger: a
   * nested button is invalid markup, and it would toggle the section on its
   * own clicks.
   */
  headerAction?: ReactNode
  /**
   * When false (the default) the header renders without disclosure chrome and
   * the body always shows — the shape the dialog surfaces want, where these
   * lists sit in an already-scrolling panel.
   */
  collapsible?: boolean
  open?: boolean
  onOpenChange?: (open: boolean) => void
  className?: string
  children: ReactNode
}

/**
 * The small "SECTION NAME 4" header plus body used by the wiki page footer's
 * three lists (sub-pages, backlinks, task backlinks) and by the credential and
 * hash details dialogs. Collapsing is opt-in per surface so the dialogs keep
 * their flat panels.
 *
 * `min-w-0` on the root lets a section sit inside a flex or grid parent
 * without long row titles forcing the column wider than its container.
 */
export function CollapsibleSection({
  title,
  count,
  headerAction,
  collapsible = false,
  open,
  onOpenChange,
  className,
  children,
}: CollapsibleSectionProps) {
  const headingClass =
    "text-xs font-medium tracking-wide text-muted-foreground uppercase"
  const label = (
    <>
      <span className="truncate">{title}</span>
      <span className="ml-1.5 shrink-0 text-muted-foreground/70">{count}</span>
    </>
  )

  if (!collapsible) {
    return (
      <div className={cn("min-w-0", className)}>
        <div className="mb-2 flex items-center justify-between gap-2">
          <h3 className={cn("flex min-w-0 items-center", headingClass)}>
            {label}
          </h3>
          {headerAction}
        </div>
        {children}
      </div>
    )
  }

  return (
    <Collapsible
      open={open}
      onOpenChange={onOpenChange}
      className={cn("min-w-0", className)}
    >
      {/* No bottom margin on the header row: the gap to the rows below belongs
          to the panel, so a folded section is exactly its header tall and
          costs nothing but the one line it shows. */}
      <div className="flex items-center justify-between gap-2">
        {/* The heading wraps the trigger rather than the reverse: a heading
            element inside a button is not phrasing content, and the section
            should still be a heading in the accessibility tree. */}
        <h3 className="min-w-0">
          <CollapsibleTrigger
            render={
              <button
                type="button"
                // -ml-1 pulls the chevron into the gutter so the title stays
                // optically aligned with the rows below it.
                className={cn(
                  "-ml-1 flex min-w-0 items-center gap-1 rounded-md px-1 py-0.5 transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                  headingClass,
                )}
              />
            }
          >
            <ChevronRightIcon
              className={cn(
                "size-3.5 shrink-0 transition-transform",
                open && "rotate-90",
              )}
            />
            {label}
          </CollapsibleTrigger>
        </h3>
        {headerAction}
      </div>
      <CollapsibleContent className="pt-2">{children}</CollapsibleContent>
    </Collapsible>
  )
}

interface SectionAddButtonProps {
  /** Tooltip text — a short imperative, e.g. "Add sub-page". */
  label: string
  /**
   * Accessible name. Defaults to `label`; pass a longer one where the tooltip
   * alone is ambiguous out of context ("Add to task" → "Add this document to
   * a task"), since a screen reader reaches the button without the heading
   * next to it.
   */
  ariaLabel?: string
  onClick: () => void
  disabled?: boolean
}

/**
 * The `+` affordance in a {@link CollapsibleSection} header. Dashed and icon-
 * only so it reads as "there could be more here" without competing with the
 * rows, and so every section's header is the same height — a labelled button
 * in one of them makes that section taller than its siblings, which is loud in
 * a folded footer where the headers sit side by side.
 */
export function SectionAddButton({
  label,
  ariaLabel,
  onClick,
  disabled,
}: SectionAddButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={onClick}
            aria-label={ariaLabel ?? label}
            className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-dashed border-border text-muted-foreground transition-colors hover:border-foreground/40 hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            disabled={disabled}
          />
        }
      >
        <PlusIcon className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

import type { ReactNode } from "react"
import { InfoIcon } from "lucide-react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { splitHelp } from "./settings-help"

// Settings forms (the endpoint and build editors, a target's dialog) lay
// rows out by the width of the form itself, not the screen: put them under an
// element with the class `@container/settings`. Narrow: label above control.
// From 32rem: label | control. Either way the help's first sentence sits
// under its control, so the eye goes down one column, and the whole help with
// the setting's key is behind an info icon by the label.

/**
 * A titled group of setting rows: a rule under the title, none between the
 * rows, which their spacing tells apart; id lets a section list scroll to
 * it. intro, when given, is said between the title and the rows. sub, for a
 * section inside a group of its own (a build's connection): a smaller title
 * and no rule, so the form's own sections stay the ruled ones; without a
 * title, the rows alone.
 */
export function SettingsSection({
  id,
  title,
  intro,
  sub = false,
  children,
}: {
  id?: string
  title?: string
  intro?: ReactNode
  sub?: boolean
  children: ReactNode
}) {
  const titleId = id && title ? `${id}-title` : undefined
  return (
    <section id={id} className={id ? "scroll-mt-6" : undefined} aria-labelledby={titleId}>
      {title &&
        (sub ? (
          <h3 id={titleId} className="text-xs font-medium text-muted-foreground">
            {title}
          </h3>
        ) : (
          <h2 id={titleId} className="border-b pb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {title}
          </h2>
        ))}
      {intro && <div className="mt-3 flex flex-col gap-2 text-sm text-muted-foreground">{intro}</div>}
      <div>{children}</div>
    </section>
  )
}

/**
 * One setting: label | control, its error, and under them the help's first
 * sentence (or hint, in its place). The info icon by the label shows the
 * whole help and the key when they say more than that line.
 */
export function SettingRow({
  id,
  label,
  fieldKey,
  required,
  help,
  hint,
  error,
  children,
}: {
  id: string
  label: string
  /** The setting's key, shown with its help. */
  fieldKey?: string
  required?: boolean
  help?: string
  /** What goes under the control instead of the help's first sentence. */
  hint?: ReactNode
  error?: string
  children: ReactNode
}) {
  const [lead, rest] = help ? splitHelp(help) : ["", ""]
  const more = !!help && (rest !== "" || !!fieldKey || hint !== undefined)
  return (
    <div className="grid gap-x-6 gap-y-1.5 py-3 @lg/settings:grid-cols-[12rem_minmax(0,1fr)] @lg/settings:items-start">
      <div className="flex items-center gap-1.5 @lg/settings:pt-1.5">
        <label htmlFor={id} className="text-sm font-medium">
          {label}
          {required && <span className="text-destructive">*</span>}
        </label>
        {more && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button type="button" className="text-muted-foreground hover:text-foreground" aria-label={`About ${label}`} />
              }
            >
              <InfoIcon className="size-3.5" />
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">
              <div className="flex flex-col gap-1 text-left">
                <span>{help}</span>
                {fieldKey && <code className="font-mono opacity-75">{fieldKey}</code>}
              </div>
            </TooltipContent>
          </Tooltip>
        )}
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        {children}
        {error && <p className="text-sm text-destructive">{error}</p>}
        {hint ?? (lead && <p className="text-xs text-muted-foreground">{lead}</p>)}
      </div>
    </div>
  )
}

import type { ReactNode } from "react"
import { InfoIcon } from "lucide-react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

// Settings forms (the endpoint and build editors) lay rows out by the
// width of the form itself, not the screen: put them under an element with
// the class `@container/settings`. Narrow: label above control. From 32rem:
// label | control, help behind an info icon. From 72rem, where a third column
// has room to read: label | control | help, the control as wide as before.

/** A titled card of setting rows; id lets a section list scroll to it. */
export function SettingsSection({ id, title, children }: { id?: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className={id ? "scroll-mt-20" : undefined} aria-labelledby={id ? `${id}-title` : undefined}>
      <h2 id={id ? `${id}-title` : undefined} className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </h2>
      <div className="divide-y rounded-xl border bg-card px-4">{children}</div>
    </section>
  )
}

/**
 * One setting: label | control and its error | help. The help column shows
 * where the form is wide enough; elsewhere the help sits behind an info icon.
 */
export function SettingRow({
  id,
  label,
  fieldKey,
  required,
  help,
  error,
  children,
}: {
  id: string
  label: string
  /** The setting's key, shown with its help. */
  fieldKey?: string
  required?: boolean
  help?: string
  error?: string
  children: ReactNode
}) {
  return (
    <div className="grid gap-x-6 gap-y-1.5 py-3 @lg/settings:grid-cols-[12rem_minmax(0,1fr)] @lg/settings:items-start @6xl/settings:grid-cols-[12rem_minmax(0,32rem)_minmax(0,1fr)]">
      <div className="flex items-center gap-1.5 @lg/settings:pt-1.5">
        <label htmlFor={id} className="text-sm font-medium">
          {label}
          {required && <span className="text-destructive">*</span>}
        </label>
        {help && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground @6xl/settings:hidden"
                  aria-label={`About ${label}`}
                />
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
      <div className="flex min-w-0 flex-col gap-1">
        {children}
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <div className="hidden min-w-0 flex-col gap-0.5 pt-1.5 text-sm text-muted-foreground @6xl/settings:flex">
        {help && <p>{help}</p>}
        {help && fieldKey && <code className="font-mono text-xs opacity-75">{fieldKey}</code>}
      </div>
    </div>
  )
}

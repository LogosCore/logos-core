import type { ReactNode } from "react"
import { TriangleAlertIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import type { BuildRecord } from "@/components/builds/build-record"

// The pieces the build modal's tabs are laid out with (build-details.tsx).

/**
 * A titled card; aside goes beside the title (the build's state, by the
 * pipeline), className onto the card (the pipeline's dotted canvas).
 */
export function Section({
  title,
  aside,
  className,
  children,
}: {
  title: string
  aside?: ReactNode
  className?: string
  children: ReactNode
}) {
  return (
    <section>
      <div className="mb-1 flex min-h-5 items-center gap-2">
        <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{title}</h2>
        {aside}
      </div>
      <div className={cn("rounded-xl border bg-card px-4", className)}>{children}</div>
    </section>
  )
}

/** A label and its value in a two-column list (a dl). */
export function Item({ label, title, children }: { label: string; title?: string; children: ReactNode }) {
  return (
    <>
      <dt className="truncate text-muted-foreground" title={title}>
        {label}
      </dt>
      <dd className="min-w-0">{children}</dd>
    </>
  )
}

/** The two-column list Items go in. */
export function ItemList({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <dl className={cn("grid grid-cols-[minmax(0,12rem)_minmax(0,1fr)] gap-x-6 gap-y-2 py-3 text-sm", className)}>
      {children}
    </dl>
  )
}

/**
 * What went wrong, and what the factory warns about with its result: on
 * the Overview tab, where a finished build opens, so they are read before
 * the minion is used, and on the Pipeline tab by the stages.
 */
export function BuildNotices({ build: b }: { build: BuildRecord }) {
  if (!b.error && b.warnings.length === 0) return null
  return (
    <div className="flex flex-col gap-3">
      {b.error && (
        <div className="flex items-start gap-2 rounded-md bg-destructive/15 p-3 text-sm text-destructive">
          <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
          <div>
            <code className="font-mono font-medium">{b.error.code}</code>: {b.error.message}
          </div>
        </div>
      )}
      {b.warnings.length > 0 && (
        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
          <TriangleAlertIcon aria-hidden className="mt-0.5 size-4 shrink-0" />
          <div className="flex min-w-0 flex-col gap-1">
            <p className="font-medium">Warnings</p>
            <ul className="list-inside list-disc">
              {b.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  )
}

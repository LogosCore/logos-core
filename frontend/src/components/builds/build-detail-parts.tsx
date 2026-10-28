import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

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

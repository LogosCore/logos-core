import { Children, type ReactNode } from "react"
import { useMediaQuery } from "@/hooks/use-media-query"
import { cn } from "@/lib/utils"

/**
 * Cards in two columns from lg up, each only as tall as its content: the
 * first, third… card on the left, the second, fourth… on the right, so they
 * read in rows as a grid's would, but none stretches to its neighbour's
 * height or waits below it. Narrower, one column in their order.
 */
export function CardColumns({ children, className }: { children: ReactNode; className?: string }) {
  // Tailwind's lg.
  const wide = useMediaQuery("(min-width: 64rem)")
  const cards = Children.toArray(children)
  if (!wide) return <div className={cn("flex flex-col gap-4", className)}>{cards}</div>
  return (
    <div className={cn("grid grid-cols-2 items-start gap-4", className)}>
      {[0, 1].map((column) => (
        <div key={column} className="flex min-w-0 flex-col gap-4">
          {cards.filter((_, i) => i % 2 === column)}
        </div>
      ))}
    </div>
  )
}

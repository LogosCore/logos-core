import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

// The pieces the "Generate" helpers of string fields are made of
// (user-agent-generator.tsx, word-generator.tsx): a labelled row of choices,
// and a choice that shows when it is the one taken.

export function Choices({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  )
}

export function Chip({
  active,
  onClick,
  title,
  children,
}: {
  active: boolean
  onClick: () => void
  title?: string
  children: ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      title={title}
      className={cn(
        "rounded-md border px-2.5 py-1 text-sm transition-colors",
        active ? "border-foreground bg-foreground text-background" : "hover:bg-muted",
      )}
    >
      {children}
    </button>
  )
}

import { cn } from "@/lib/utils"

/** A tab in a pane's tool row: the console's task tabs, a file's views. */
export function TabButton({ active, onClick, title, children }: { active: boolean; onClick: () => void; title?: string; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "cursor-pointer rounded px-2 py-0.5 text-xs font-medium",
        active ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  )
}

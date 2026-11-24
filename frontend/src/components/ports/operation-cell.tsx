import { SwordsIcon } from "lucide-react"

/**
 * A Ports row's operation, when the page lists every operation the user is
 * a member of (no operation picked), as Findings' Operation column.
 */
export function OperationCell({ name }: { name: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-muted-foreground" title={name}>
      <SwordsIcon className="size-3.5 shrink-0" />
      <span className="truncate">{name || "—"}</span>
    </span>
  )
}

import { Loader2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { TARGET_STATE_HELP, TARGET_STATE_LABEL, TARGET_STATE_STYLE } from "./target-values"

/** A target's state; spinning while its first call is on its way. */
export function TargetStateBadge({ state, pending }: { state: string; pending?: boolean }) {
  return (
    <Badge variant="outline" className={cn("gap-1", TARGET_STATE_STYLE[state])} title={TARGET_STATE_HELP[state]}>
      {(pending || state === "new") && <Loader2Icon className="size-3 animate-spin" />}
      {TARGET_STATE_LABEL[state] ?? state}
    </Badge>
  )
}

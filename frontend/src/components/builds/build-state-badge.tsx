import { Loader2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { isUnfinished } from "@/graphql/hooks/builds"
import { STATE_LABEL } from "./build-values"

const STATE_STYLE: Record<string, string> = {
  QUEUED: "border-muted-foreground/30 text-muted-foreground",
  BUILDING: "border-sky-600/40 text-sky-700 dark:text-sky-400",
  SUCCEEDED: "border-emerald-600/40 text-emerald-700 dark:text-emerald-400",
  FAILED: "border-destructive/40 text-destructive",
  CANCELLED: "border-muted-foreground/30 text-muted-foreground",
}

/** A build's state. Where it is in its pipeline is the pipeline's to show. */
export function BuildStateBadge({ build }: { build: { state: string } }) {
  return (
    <Badge variant="outline" className={cn("gap-1", STATE_STYLE[build.state])}>
      {isUnfinished(build.state) && <Loader2Icon className="size-3 animate-spin" />}
      {STATE_LABEL[build.state] ?? build.state}
    </Badge>
  )
}

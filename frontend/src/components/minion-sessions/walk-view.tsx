import { FolderTreeIcon, LoaderIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { cn } from "@/lib/utils"
import type { DirWalk } from "./dir-walk"
import { FINISHED } from "./task-actions"

// A dir_walk task's view: where the walk stands, as the minion last said.
// Its listings went to the machine's file browser as they came, so that
// is where what it saw is read.

export function WalkView({
  task: t,
  walk: w,
  onOpenInFileBrowser,
}: {
  task: MinionTaskFieldsFragment
  walk: DirWalk
  /** Opens the folder walked in the machine's file browser. */
  onOpenInFileBrowser?: (path: string) => void
}) {
  const running = !FINISHED.has(t.state)
  const n = (v: number) => v.toLocaleString("en-US")
  return (
    <div className="flex min-h-0 flex-1 items-start overflow-auto">
      <div className="flex w-full max-w-3xl flex-col gap-4 p-4">
        {onOpenInFileBrowser && (
          <div className="flex flex-wrap items-center gap-2">
            <Button onClick={() => onOpenInFileBrowser(w.root)}>
              <FolderTreeIcon />
              Open in File Browser
            </Button>
          </div>
        )}
        <InfoCard labelWidth="w-32">
          <LabelRow label="Walked">
            <span className="font-mono text-xs break-all">{w.root}</span>
          </LabelRow>
          <LabelRow label="Folders listed">
            <span className="tabular-nums">{n(w.folders)}</span>
          </LabelRow>
          <LabelRow label="Entries">
            <span className="tabular-nums">{n(w.entries)}</span>
          </LabelRow>
          <LabelRow label="Not read">
            <span className={cn("tabular-nums", w.unread === 0 && "text-muted-foreground")}>{n(w.unread)}</span>
            {w.unread > 0 && <span className="text-muted-foreground"> · not allowed, or gone; a red lock in the file browser</span>}
          </LabelRow>
          <LabelRow label="Not entered">
            <span className={cn("tabular-nums", w.skipped === 0 && "text-muted-foreground")}>{n(w.skipped)}</span>
            {w.skipped > 0 && <span className="text-muted-foreground"> · deeper than it went, past its limit, or on another file system</span>}
          </LabelRow>
          <LabelRow label={running ? "Reading" : "State"}>
            {running ? (
              w.current ? (
                <span className="flex min-w-0 items-start gap-1.5">
                  <LoaderIcon className="mt-0.5 size-3.5 shrink-0 animate-spin text-muted-foreground" />
                  <span className="font-mono text-xs break-all">{w.current}</span>
                </span>
              ) : (
                <span className="text-muted-foreground">starting…</span>
              )
            ) : t.state === "DONE" && t.ok !== false ? (
              <span className="text-emerald-600 dark:text-emerald-400">Done</span>
            ) : (
              <span className="text-red-600 dark:text-red-400">{t.state === "CANCELLED" || t.err === "cancelled" ? "Stopped" : (t.err ?? "Failed")}</span>
            )}
          </LabelRow>
        </InfoCard>
      </div>
    </div>
  )
}

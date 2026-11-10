import { DownloadIcon, SparklesIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { SKILL_DOWNLOAD_URL } from "@/constants/skill"
import type { SkillUpdateDecision } from "@/lib/skill-update"

/**
 * The built-in skill's update prompt itself (SkillUpdateDialog decides when):
 * a chunk of its own, loaded the first time it shows, not on every page's
 * first paint.
 */
export default function SkillUpdatePrompt({
  decision,
  dismiss,
  handleDownload,
}: {
  decision: SkillUpdateDecision
  dismiss: () => void
  handleDownload: () => void
}) {
  return (
    <Dialog open={decision.show} onOpenChange={(next) => { if (!next) dismiss() }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <SparklesIcon className="size-4 text-muted-foreground" />
            The agent skill has been updated
          </DialogTitle>
          <DialogDescription>
            Your agent's MCP tools are already up to date — this only updates
            the packaged guidance it reads. Re-download to give it the latest.
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[45vh] space-y-4 overflow-y-auto">
          {decision.pending.map((release) => (
            <div key={release.version} className="space-y-1.5">
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-sm font-medium">Version {release.version}</span>
                <span className="text-xs text-muted-foreground">{release.date}</span>
              </div>
              <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
                {release.notes.map((note, i) => (
                  <li key={i}>{note}</li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={dismiss}>
            Remind me later
          </Button>
          <Button
            type="button"
            onClick={handleDownload}
            render={<a href={SKILL_DOWNLOAD_URL} download />}
          >
            <DownloadIcon className="size-4" />
            Download skill
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

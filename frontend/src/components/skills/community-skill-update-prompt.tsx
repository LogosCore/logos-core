import { PackageIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import type { OutdatedSkill } from "@/lib/community-skill-update"

/**
 * The community skills' update prompt itself (CommunitySkillUpdateDialog
 * decides when): a chunk of its own, loaded the first time it shows, not on
 * every page's first paint.
 */
export default function CommunitySkillUpdatePrompt({
  stale,
  dismiss,
  openSkills,
}: {
  stale: OutdatedSkill[]
  dismiss: () => void
  openSkills: () => void
}) {
  return (
    <Dialog open onOpenChange={(next) => { if (!next) dismiss() }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <PackageIcon className="size-4 text-muted-foreground" />
            {stale.length === 1
              ? "A skill you use has been updated"
              : `${stale.length} skills you use have been updated`}
          </DialogTitle>
          <DialogDescription>
            Published by other operators on this server. Your copy stays as it
            is until you download the new one.
          </DialogDescription>
        </DialogHeader>

        <ul className="max-h-[45vh] space-y-3 overflow-y-auto">
          {stale.map((skill) => (
            <li key={skill.name} className="space-y-0.5">
              <div className="flex items-baseline justify-between gap-2">
                <span className="text-sm font-medium">{skill.name}</span>
                <span className="text-xs text-muted-foreground tabular-nums">
                  v{skill.installedVersion} → v{skill.currentVersion}
                </span>
              </div>
              <p className="text-xs text-muted-foreground">
                by {skill.ownerUsername}
                {skill.description ? ` — ${skill.description}` : ""}
              </p>
            </li>
          ))}
        </ul>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={dismiss}>
            Remind me later
          </Button>
          <Button type="button" onClick={openSkills}>
            Open skills
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

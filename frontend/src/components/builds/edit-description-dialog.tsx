import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useUpdateBuildDescription } from "@/graphql/hooks/builds"

/** What the dialog needs of a build: the table's rows and the build page's record both have it. */
export interface DescribedBuild {
  id: string
  description?: string | null
}

/**
 * Changes what a build is for: the operators' note, core's own, so any
 * state of the build takes it. Open while build is set.
 */
export function EditDescriptionDialog({ build, onClose }: { build: DescribedBuild | null; onClose: () => void }) {
  return (
    <Dialog open={build !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        {build && <EditDescriptionForm key={build.id} buildId={build.id} description={build.description ?? ""} onDone={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function EditDescriptionForm({ buildId, description, onDone }: { buildId: string; description: string; onDone: () => void }) {
  const update = useUpdateBuildDescription()
  const [text, setText] = useState(description)
  const trimmed = text.trim()
  const unchanged = trimmed === description.trim()

  async function save() {
    try {
      await update.mutateAsync({ id: buildId, description: trimmed })
      toast.success("Description saved")
      onDone()
    } catch (err) {
      toast.error("Could not save the description", { description: errorMessage(err, "Request failed") })
    }
  }

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (trimmed && !unchanged) void save()
      }}
    >
      <DialogHeader>
        <DialogTitle>Edit description</DialogTitle>
        <DialogDescription>What this build is for, for the rest of the team. The factory and the minion never get it.</DialogDescription>
      </DialogHeader>
      <Textarea
        autoFocus
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) e.currentTarget.form?.requestSubmit()
        }}
        rows={3}
        maxLength={500}
        aria-label="Description"
      />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={!trimmed || unchanged || update.isPending}>
          {update.isPending ? "Saving..." : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

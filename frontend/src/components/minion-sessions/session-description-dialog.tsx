import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useSetMinionSessionDescription } from "@/graphql/hooks/minion-sessions"
import { errorMessage } from "@/components/modules/management/api-errors"

// A session's description, edited: what the session is for, the operators'
// note. It starts as the build's description; empty clears it.

const MAX = 500

export function SessionDescriptionDialog({
  session,
  onClose,
}: {
  session: MinionSessionFieldsFragment | null
  onClose: () => void
}) {
  return (
    <Dialog open={session !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {/* Keyed by the session: each opening starts from its description. */}
        {session && <Editor key={session.id} session={session} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function Editor({ session, onClose }: { session: MinionSessionFieldsFragment; onClose: () => void }) {
  const save = useSetMinionSessionDescription()
  const [text, setText] = useState(session.description)
  const [error, setError] = useState<string | null>(null)
  const length = [...text.trim()].length
  const tooLong = length > MAX
  const submit = async () => {
    setError(null)
    try {
      await save.mutateAsync({ id: session.id, description: text.trim() })
      onClose()
    } catch (err) {
      setError(errorMessage(err, "Failed to save the description"))
    }
  }
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (!tooLong) void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>Session #{session.number}</DialogTitle>
        <DialogDescription>
          What this session is for, for the rest of the team. It started as the build&apos;s description.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1">
        <Textarea
          autoFocus
          rows={4}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            // Ctrl/Cmd+Enter saves, as in the other editors.
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && !tooLong) {
              e.preventDefault()
              void submit()
            }
          }}
          aria-label="Description"
        />
        <span className={`self-end text-xs ${tooLong ? "text-destructive" : "text-muted-foreground"}`}>
          {length} / {MAX}
        </span>
      </div>
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={tooLong || save.isPending}>
          {save.isPending ? "Saving..." : "Save"}
        </Button>
      </DialogFooter>
    </form>
  )
}

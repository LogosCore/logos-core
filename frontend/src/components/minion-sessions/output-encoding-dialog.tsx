import { useState } from "react"
import { CheckIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SearchInput } from "@/components/ui/search-input"
import { useMinionTaskOutputDecodings } from "@/graphql/hooks/minion-tasks"
import { useSetMinionSessionOutputEncoding } from "@/graphql/hooks/minion-sessions"
import { errorMessage } from "@/components/modules/management/api-errors"
import { cn } from "@/lib/utils"
import { firstLine, groupDecodings, UTF8, visibleText, type DecodingGroup } from "./output-encoding-values"

// The Encodings table, as CyberChef's Text Encoding Brute Force has it: a
// piece of a task's output (from the line where it stops being ASCII) read
// in every encoding core knows, the likely ones first. A row picked shows
// its piece whole; an operator makes it what the session's output is read
// as, every task's, for the whole team. The download keeps the bytes.

export function OutputEncodingDialog({
  open,
  onClose,
  taskId,
  version,
  session,
  canPick,
}: {
  open: boolean
  onClose: () => void
  taskId: string
  /** Changes when more output came: the table is read again. */
  version: string
  session: { id: string; number: number; outputEncoding?: string | null } | null | undefined
  /** Whether the caller sets the session's encoding: an operator. */
  canPick: boolean
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[90vh] flex-col sm:max-w-4xl">
        {open && <Table taskId={taskId} version={version} session={session} canPick={canPick} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function Table({
  taskId,
  version,
  session,
  canPick,
  onClose,
}: {
  taskId: string
  version: string
  session: { id: string; number: number; outputEncoding?: string | null } | null | undefined
  canPick: boolean
  onClose: () => void
}) {
  const q = useMinionTaskOutputDecodings(taskId, version, true)
  const save = useSetMinionSessionOutputEncoding()
  const [filter, setFilter] = useState("")
  const current = session?.outputEncoding || UTF8
  const [picked, setPicked] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const all = q.data?.minionTask?.outputDecodings ?? []
  // Those that read it alike are one row (plain ASCII: every 8-bit one).
  const rows = groupDecodings(all, filter, current)
  // The row picked, else the session's, else the likeliest.
  const shownId = picked ?? current
  const shown = rows.find((g) => g.members.some((d) => d.encoding === shownId)) ?? rows[0]

  const use = async (encoding: string) => {
    if (!session) return
    setError(null)
    try {
      await save.mutateAsync({ id: session.id, encoding: encoding === UTF8 ? null : encoding })
      onClose()
    } catch (err) {
      setError(errorMessage(err, "Could not set the encoding"))
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Read the output as</DialogTitle>
        <DialogDescription>
          A piece of the output in each encoding core knows, the likeliest first.
          {session ? ` The one picked reads every task of session #${session.number}, for the whole team.` : ""}
        </DialogDescription>
      </DialogHeader>
      <SearchInput value={filter} onValueChange={setFilter} debounceMs={0} placeholder="Find an encoding (866, cyrillic, windows)" title="Its name or id" />
      <div className="min-h-40 flex-1 overflow-y-auto rounded-md border" role="listbox" aria-label="Encodings">
        {q.isLoading ? (
          <p className="p-3 text-sm text-muted-foreground">Reading…</p>
        ) : q.error ? (
          <p className="p-3 text-sm text-destructive">{errorMessage(q.error, "Could not read the output")}</p>
        ) : rows.length === 0 ? (
          <p className="p-3 text-sm text-muted-foreground">No encoding matches.</p>
        ) : (
          rows.map((g) => (
            <button
              key={g.encoding}
              type="button"
              role="option"
              aria-selected={g === shown}
              onClick={() => setPicked(g.encoding)}
              onDoubleClick={() => canPick && void use(g.encoding)}
              title={canPick ? "Shows it whole below; a double click uses it" : "Shows it whole below"}
              className={cn(
                "grid w-full cursor-pointer grid-cols-[15rem_minmax(0,1fr)_3.5rem] items-baseline gap-3 border-b px-3 py-1 text-left text-xs last:border-b-0 hover:bg-muted/60",
                g === shown && "bg-accent text-accent-foreground shadow-[inset_2px_0_0_var(--foreground)] hover:bg-accent",
                g !== shown && g.unreadable > 0 && "text-muted-foreground",
              )}
            >
              <span className="inline-flex min-w-0 items-baseline gap-1.5">
                <span className="truncate">{g.name}</span>
                {g.members.length > 1 && (
                  <span className="shrink-0 text-muted-foreground tabular-nums" title={`Read it the same: ${othersOf(g)}`}>
                    +{g.members.length - 1}
                  </span>
                )}
                {g.encoding === current && <CheckIcon className="size-3 shrink-0 self-center text-emerald-600 dark:text-emerald-400" aria-label="in use" />}
              </span>
              <span className="truncate font-mono">{firstLine(g.text)}</span>
              <span className="text-right tabular-nums" title="Signs it is not the output's: characters that could not be read, control characters, line breaks lost">
                {g.unreadable > 0 ? g.unreadable : ""}
              </span>
            </button>
          ))
        )}
      </div>
      {shown && (
        <div className="flex flex-none flex-col gap-1">
          <span className="truncate text-xs text-muted-foreground" title={shown.members.length > 1 ? `Read it the same: ${othersOf(shown)}` : undefined}>
            {shown.name}
            {shown.encoding === current ? ", in use" : ""}
            {shown.members.length > 1 ? ` (and ${shown.members.length - 1} more that read it the same)` : ""}
            {shown.unreadable > 0 ? `: ${shown.unreadable} ${shown.unreadable === 1 ? "sign" : "signs"} it is not the output's` : ""}
          </span>
          <pre className="h-36 overflow-auto rounded-md border bg-muted/40 p-2 font-mono text-xs break-all whitespace-pre-wrap">
            {visibleText(shown.text)}
          </pre>
        </div>
      )}
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Close
        </Button>
        {canPick && session && (
          <Button type="button" disabled={!shown || shown.encoding === current || save.isPending} onClick={() => shown && void use(shown.encoding)}>
            {save.isPending ? "Saving..." : shown && shown.encoding !== current ? `Read as ${shown.name}` : "In use"}
          </Button>
        )}
      </DialogFooter>
    </>
  )
}

/** The other encodings of a row, by name. */
function othersOf(g: DecodingGroup): string {
  return g.members
    .slice(1)
    .map((d) => d.name)
    .join(", ")
}

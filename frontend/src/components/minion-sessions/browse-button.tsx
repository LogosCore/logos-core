import { useState } from "react"
import { ChevronDownIcon, FolderOpenIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import type { ConsoleCommand } from "./command-bar"
import { chooseHandlerCommand, handlerCommands, readPicked, savePicked } from "./handler-commands"

// Browse files: a session's dir_listing command run from the console, with
// its defaults alone (the handler's params, the rest their defaults: the
// minion's working directory, or wherever the command's path defaults to).
// The listing shows as a file browser, its folders opening from there.
// With several dir_listing commands, the one the operator picks for the
// session, else the factory's first (handler-commands.ts); the menu beside
// it picks another, and runs it.

const HANDLER = "dir_listing"

export function BrowseButton({
  sessionId,
  commands,
  onQueued,
}: {
  sessionId: string
  commands: readonly ConsoleCommand[]
  onQueued?: (task: MinionTaskFieldsFragment) => void
}) {
  const queue = useQueueMinionTask()
  const [picked, setPicked] = useState(() => readPicked(sessionId, HANDLER))
  const candidates = handlerCommands(commands, HANDLER)
  const chosen = chooseHandlerCommand(candidates, picked)
  if (!chosen) return null

  const browse = (id: string) =>
    queue.mutate(
      { sessionId, command: id, args: null },
      { onSuccess: (data) => onQueued?.(data.queueMinionTask), onError: (err) => toast.error(errorMessage(err, "Could not list the files")) },
    )

  return (
    <span className="flex shrink-0 items-center">
      <Button
        variant="ghost"
        size="icon-xs"
        title={candidates.length > 1 ? `Browse files, with ${chosen.id}` : "Browse files"}
        aria-label="Browse files"
        disabled={queue.isPending}
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => browse(chosen.id)}
      >
        <FolderOpenIcon />
      </Button>
      {candidates.length > 1 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-xs" className="w-4" title="Browse with another command" aria-label="Browse with another command" />
            }
          >
            <ChevronDownIcon />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="min-w-56">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Browse files with</DropdownMenuLabel>
              <DropdownMenuRadioGroup
                value={chosen.id}
                onValueChange={(v) => {
                  if (typeof v !== "string") return
                  savePicked(sessionId, HANDLER, v)
                  setPicked(v)
                  browse(v)
                }}
              >
                {candidates.map((c) => (
                  <DropdownMenuRadioItem key={c.id} value={c.id}>
                    <span className="truncate">{c.label}</span>
                    <span className="ml-auto font-mono text-xs text-muted-foreground">{c.id}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </span>
  )
}

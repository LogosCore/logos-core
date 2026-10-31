import { UploadIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import type { MenuCommand } from "./action-menu"

// A folder's Upload here, in a view's tool row: with one upload command,
// a click runs it; with several (upload and stream_upload, say), a menu of
// them, as the entries' context menus offer.

export function UploadHereButton({ folder, commands, onRun }: { folder: string; commands: readonly MenuCommand[]; onRun: (commandId: string) => void }) {
  if (commands.length === 0) return null
  if (commands.length === 1)
    return (
      <Button variant="ghost" size="icon-xs" aria-label="Upload here" title={`Upload a file to ${folder} (${commands[0].id})`} onClick={() => onRun(commands[0].id)}>
        <UploadIcon />
      </Button>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon-xs" aria-label="Upload here" title={`Upload a file to ${folder}`}>
            <UploadIcon />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        {commands.map((c) => (
          <DropdownMenuItem key={c.id} onClick={() => onRun(c.id)}>
            <span className="truncate">{c.label}</span>
            <span className="ml-auto pl-4 font-mono text-xs text-muted-foreground">{c.id}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

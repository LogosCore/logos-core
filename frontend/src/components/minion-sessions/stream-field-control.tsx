import { useRef } from "react"
import { FileIcon, TerminalIcon, UploadIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import type { Draft } from "@/components/modules/management/field-values"
import { formatBytes } from "@/lib/format-bytes"

// A stream param (docs: contracts/minion-build.md, a command's `stream`):
// a file picked in this browser, sent to the minion as it goes once the
// task is queued, of any size and kept nowhere; or, via curl, its name,
// with the curl command in the task's view.

export function StreamFieldControl({
  id,
  draft,
  onChange,
  disabled,
  invalid,
}: {
  id: string
  draft: Extract<Draft, { kind: "stream" }>
  onChange: (d: Draft) => void
  disabled?: boolean
  invalid?: boolean
}) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={draft.curl} disabled={disabled} onCheckedChange={(curl) => onChange({ ...draft, curl })} />
        <TerminalIcon className="size-4 text-muted-foreground" />
        Upload it with curl
      </label>
      {draft.curl ? (
        <>
          <Input
            id={id}
            className="font-mono"
            placeholder="The file's name, e.g. tool.exe"
            value={draft.name}
            disabled={disabled}
            aria-invalid={invalid || undefined}
            onChange={(e) => onChange({ ...draft, name: e.target.value })}
          />
          <span className="text-xs text-muted-foreground">The task's view gives the curl command once it is queued.</span>
        </>
      ) : (
        <>
          {draft.file && (
            <div className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm" aria-invalid={invalid || undefined}>
              <FileIcon className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 truncate font-medium">{draft.file.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{formatBytes(draft.file.size)}</span>
              <Button type="button" variant="ghost" size="icon-sm" className="ml-auto" title="Remove" aria-label="Remove"
                disabled={disabled} onClick={() => onChange({ ...draft, file: null })}>
                <XIcon />
              </Button>
            </div>
          )}
          <input
            ref={input}
            type="file"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0]
              e.target.value = ""
              if (file) onChange({ ...draft, file })
            }}
          />
          <Button id={id} type="button" variant="outline" size="sm" className="self-start" disabled={disabled} onClick={() => input.current?.click()}>
            <UploadIcon />
            {draft.file ? "Pick another" : "Pick a file"}
          </Button>
          <span className="text-xs text-muted-foreground">Any size: sent to the minion as it goes once the task is queued, kept nowhere. Keep this tab open while it goes.</span>
        </>
      )}
    </div>
  )
}

import { useRef } from "react"
import { FileIcon, UploadIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { Draft } from "@/components/modules/management/field-values"
import { formatBytes } from "@/lib/format-bytes"

// A stream param (docs: contracts/minion-build.md, a command's `stream`): a
// file picked in this browser, sent to the minion as it goes once the task
// is queued, of any size and kept nowhere. None picked, it is uploaded with
// curl from the task's view, and the path names it.

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
  const named = !draft.file && draft.name.trim() !== ""
  return (
    <div className="flex flex-col gap-2">
      {(draft.file || named) && (
        <div className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm" aria-invalid={invalid || undefined}>
          <FileIcon className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate font-medium">{draft.file?.name ?? draft.name}</span>
          <span className="shrink-0 text-xs text-muted-foreground">{draft.file ? formatBytes(draft.file.size) : "named, for curl"}</span>
          <Button type="button" variant="ghost" size="icon-sm" className="ml-auto" title="Remove" aria-label="Remove"
            disabled={disabled} onClick={() => onChange({ kind: "stream", file: null, name: "" })}>
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
          if (file) onChange({ kind: "stream", file, name: "" })
        }}
      />
      <Button id={id} type="button" variant="outline" size="sm" className="self-start" disabled={disabled} onClick={() => input.current?.click()}>
        <UploadIcon />
        {draft.file ? "Pick another" : "Pick a file"}
      </Button>
      <span className="text-xs text-muted-foreground">
        {draft.file
          ? "Sent from this tab as it goes, once the task is queued: any size, kept nowhere. Keep the tab open."
          : "Or pick none, and upload it with curl from the task's view: then the path names the file."}
      </span>
    </div>
  )
}

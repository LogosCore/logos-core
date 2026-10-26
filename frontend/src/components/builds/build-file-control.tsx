import { useContext, useRef, useState } from "react"
import { FileIcon, UploadIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { formatBytes } from "@/lib/format-bytes"
import type { Draft, FieldDef } from "@/components/modules/management/field-values"
import { BuildFilesContext } from "./build-files-context"
import { acceptText, fileFits } from "./build-values"

/**
 * A file field: the chosen file, and a way to upload another or pick one the
 * operation already has. Files that do not fit the field (accept, max size)
 * are refused before they are sent, and left out of the list.
 */
export function BuildFileControl({
  id,
  field,
  draft,
  onChange,
  disabled,
  invalid,
}: {
  id: string
  field: FieldDef
  draft: Extract<Draft, { kind: "file" }>
  onChange: (d: Draft) => void
  disabled?: boolean
  invalid?: boolean
}) {
  const ctx = useContext(BuildFilesContext)
  const input = useRef<HTMLInputElement>(null)
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  if (!ctx) return <span className="text-sm text-muted-foreground">Files are set in a build.</span>
  const chosen = draft.id ? ctx.files.find((f) => f.id === draft.id) : undefined
  const fitting = ctx.files.filter((f) => f.id !== draft.id && fileFits(field, f) === null)
  const busy = progress !== null

  async function upload(file: File) {
    setError(null)
    const misfit = fileFits(field, { name: file.name, size: file.size, mediaType: file.type })
    if (misfit) {
      setError(misfit)
      return
    }
    setProgress(0)
    try {
      const f = await ctx!.upload(file, setProgress)
      onChange({ kind: "file", id: f.id })
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upload failed")
    } finally {
      setProgress(null)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {draft.id && (
        <div className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm" aria-invalid={invalid || undefined}>
          <FileIcon className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate font-medium">{chosen ? chosen.name : "A file this operation no longer has"}</span>
          {chosen && <span className="shrink-0 text-xs text-muted-foreground">{formatBytes(chosen.size)}</span>}
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="ml-auto"
            onClick={() => onChange({ kind: "file", id: null })}
            disabled={disabled || busy}
            title="Remove"
            aria-label="Remove"
          >
            <XIcon />
          </Button>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <input
          ref={input}
          type="file"
          className="hidden"
          accept={field.accept?.join(",") || undefined}
          onChange={(e) => {
            const file = e.target.files?.[0]
            e.target.value = ""
            if (file) void upload(file)
          }}
        />
        <Button
          id={id}
          type="button"
          variant="outline"
          size="sm"
          onClick={() => input.current?.click()}
          disabled={disabled || busy}
        >
          <UploadIcon />
          {busy ? `Uploading ${Math.round((progress ?? 0) * 100)}%` : draft.id ? "Upload another" : "Upload"}
        </Button>
        {fitting.length > 0 && (
          <Select
            value={null}
            onValueChange={(v) => {
              if (typeof v === "string") onChange({ kind: "file", id: v })
            }}
            disabled={disabled || busy}
          >
            <SelectTrigger className="w-full max-w-64" aria-label="Use an uploaded file">
              <SelectValue placeholder="Or use an uploaded file" />
            </SelectTrigger>
            <SelectContent>
              {fitting.map((f) => (
                <SelectItem key={f.id} value={f.id} description={formatBytes(f.size)}>
                  {f.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
      <span className="text-xs text-muted-foreground">{acceptText(field)}</span>
      {error && <span className="text-sm text-destructive">{error}</span>}
    </div>
  )
}

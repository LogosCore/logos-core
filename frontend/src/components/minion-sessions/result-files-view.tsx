import { useState } from "react"
import { DownloadIcon, ListIcon } from "lucide-react"
import { FileTypeIcon } from "@/components/file-preview/file-type-icon"
import { canonicalContentType } from "@/components/file-preview/kind"
import { fileTypeName } from "@/components/file-preview/type-name"
import { Button } from "@/components/ui/button"
import type { MinionFileFieldsFragment } from "@/graphql/gql/graphql"
import { formatBytes } from "@/lib/format-bytes"
import { FileView } from "./file-view"
import { savedName } from "./verify-commands"

// A factory command's task's files (docs: contracts/factory-commands): the
// ones its factory's result named, which core keeps as the task's. One shows
// as a download's file does (FileView: its preview, overview, bytes and
// strings); several as a list, each opening there, the list a click away.

const API_URL = import.meta.env.VITE_API_URL as string

export function ResultFilesView({ files }: { files: readonly MinionFileFieldsFragment[] }) {
  const [picked, setPicked] = useState<string | null>(null)
  const open = files.length === 1 ? files[0] : files.find((f) => f.id === picked)
  if (open) {
    return (
      <FileView
        key={open.id}
        file={open}
        end={
          files.length > 1 && (
            <Button variant="ghost" size="icon-xs" title={`All its ${files.length} files`} aria-label="Show all its files" onClick={() => setPicked(null)}>
              <ListIcon />
            </Button>
          )
        }
      />
    )
  }
  return (
    <ul className="min-h-0 flex-1 divide-y overflow-auto">
      {files.map((f) => {
        // As its download is saved: core's name for it.
        const name = savedName(f.name, `file-${f.id.slice(0, 8)}`)
        const contentType = canonicalContentType(f.contentType) || "application/octet-stream"
        const type = fileTypeName(contentType, name)
        return (
          <li key={f.id} className="flex items-baseline gap-2 px-3 py-1.5">
            <FileTypeIcon contentType={contentType} filename={name} className="size-3.5 shrink-0 self-center text-muted-foreground" />
            <button
              type="button"
              onClick={() => setPicked(f.id)}
              title={`Open ${f.name}`}
              className="min-w-0 cursor-pointer truncate text-left font-mono text-xs font-medium hover:underline"
            >
              {name}
            </button>
            <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{formatBytes(f.size)}</span>
            {type && <span className="hidden min-w-0 truncate text-xs text-muted-foreground sm:inline">{type}</span>}
            <Button
              variant="ghost"
              size="icon-xs"
              className="ml-auto shrink-0 self-center"
              title={`Download ${name}`}
              aria-label={`Download ${name}`}
              nativeButton={false}
              render={<a href={`${API_URL}/minion-files/${encodeURIComponent(f.id)}`} download />}
            >
              <DownloadIcon />
            </Button>
          </li>
        )
      })}
    </ul>
  )
}

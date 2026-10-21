// What a chip opens in the HTML bundle: the record the exporter wrote onto
// the node, in full, with a copy button per value. The app's chips open
// details dialogs that load the record from the server; here it is already
// in the page's data.

import type { ReactNode } from "react"
import { CopyIcon } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"

export interface RecordRow {
  label: string
  value: string
  /** Monospace: usernames, passwords, hashes, addresses. */
  mono?: boolean
  /** Multi-line content shown as a block, e.g. a private key. */
  block?: boolean
}

interface RecordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  icon: ReactNode
  title: string
  description?: string
  rows: RecordRow[]
  tags?: string[]
}

export function RecordDialog({ open, onOpenChange, icon, title, description, rows, tags }: RecordDialogProps) {
  const shown = rows.filter((r) => r.value)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {icon}
            <span className="min-w-0 break-words">{title}</span>
          </DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <dl className="flex flex-col gap-3 text-sm">
          {shown.map((row, i) => (
            <div key={`${row.label}-${i}`} className="flex flex-col gap-1">
              <dt className="text-xs font-medium text-muted-foreground">{row.label}</dt>
              <dd className="flex items-start gap-2">
                {row.block ? (
                  <pre className="max-h-64 min-w-0 flex-1 overflow-auto rounded-md bg-muted p-2 font-mono text-xs whitespace-pre-wrap break-all">
                    {row.value}
                  </pre>
                ) : (
                  <span className={cn("min-w-0 flex-1 break-all select-all", row.mono && "font-mono")}>
                    {row.value}
                  </span>
                )}
                <button
                  type="button"
                  onClick={() => void copyToClipboard(row.value, row.label.toLowerCase())}
                  className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                  title={`Copy ${row.label.toLowerCase()}`}
                  aria-label={`Copy ${row.label.toLowerCase()}`}
                >
                  <CopyIcon className="size-3.5" />
                </button>
              </dd>
            </div>
          ))}
          {tags && tags.length > 0 && (
            <div className="flex flex-col gap-1">
              <dt className="text-xs font-medium text-muted-foreground">Tags</dt>
              <dd className="flex flex-wrap gap-1">
                {tags.map((t) => (
                  <span key={t} className="rounded-md border px-1.5 py-0.5 text-xs">
                    {t}
                  </span>
                ))}
              </dd>
            </div>
          )}
          {shown.length === 0 && !tags?.length && (
            <p className="text-muted-foreground">Nothing else was recorded for this item.</p>
          )}
        </dl>
      </DialogContent>
    </Dialog>
  )
}

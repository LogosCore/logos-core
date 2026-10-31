import { useRef, useState } from "react"
import { SquareIcon, TerminalIcon, UploadIcon } from "lucide-react"
import { toast } from "sonner"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { fetchMinionStreamLink } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatBytes } from "@/lib/format-bytes"
import { startStreamUpload, stopStreamUpload, useStreamUpload } from "@/lib/stream-upload"
import { shellQuote } from "./verify-commands"

// The uploader of an upload_stream task (docs:
// contracts/task-results-and-actions.md, Uploading a stream): this
// browser's upload as it goes, or the ways to start one, or go on with
// one that stopped: pick the file here, or copy a curl command (from where
// core has it, after a drop).

const API_URL = import.meta.env.VITE_API_URL as string

type Task = MinionTaskFieldsFragment
type Upload = NonNullable<Task["upload"]>

export function StreamUploader({ task: t, upload: up }: { task: Task; upload: Upload }) {
  const mine = useStreamUpload(t.id)
  const input = useRef<HTMLInputElement>(null)
  const [copying, setCopying] = useState(false)
  const open = up.uploaderState === "WAITING" || up.uploaderState === "PAUSED" || up.uploaderState === "UPLOADING"
  const going = mine?.state === "uploading" || mine?.state === "retrying"

  const pick = (file: File) => {
    if (up.sized && file.size !== up.size) {
      toast.error(`${file.name} is ${formatBytes(file.size)}: the stream is ${formatBytes(up.size)}. Pick the same file.`)
      return
    }
    if (file.name !== up.name) toast.info(`The file goes on the host as ${up.name}, the name the task was given.`)
    startStreamUpload(t.id, file)
  }

  const copyCurl = async () => {
    setCopying(true)
    try {
      const path = await fetchMinionStreamLink(t.id)
      const link = new URL(`${API_URL}${path}`, window.location.origin).toString()
      const from = up.received > 0 ? `-C ${up.received} ` : ""
      await copyToClipboard(`curl -f ${from}-T ${shellQuote(up.name)} ${shellQuote(link)}`, "curl command")
    } catch (err) {
      toast.error(errorMessage(err, "Could not make a link"))
    } finally {
      setCopying(false)
    }
  }

  if (going) {
    return (
      <div className="flex flex-col gap-1 text-xs text-muted-foreground">
        <span className="tabular-nums">
          {mine.state === "retrying" ? `Sending from this browser: retrying (${mine.error})` : "Sending from this browser"} ·{" "}
          {formatBytes(mine.sent)} of {formatBytes(mine.size)}
        </span>
        <Button variant="outline" size="xs" className="self-start" onClick={() => stopStreamUpload(t.id)}
          title="Stop sending: the stream keeps what core has, to go on later">
          <SquareIcon />
          Stop sending
        </Button>
      </div>
    )
  }
  if (!open) return null
  return (
    <div className="flex flex-col gap-2">
      {mine?.state === "failed" && <p className="text-xs text-red-600 dark:text-red-400">This browser's upload stopped: {mine.error}</p>}
      {up.uploaderState === "UPLOADING" ? (
        <p className="text-xs text-muted-foreground">Another client is sending it.</p>
      ) : (
        <p className="text-xs text-muted-foreground">
          {up.uploaderState === "PAUSED"
            ? `Its uploader went away at ${formatBytes(up.received)}: send the rest from here, or with curl.`
            : "The minion waits for the file: send it from here, or with curl."}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <input
          ref={input}
          type="file"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0]
            e.target.value = ""
            if (file) pick(file)
          }}
        />
        <Button onClick={() => input.current?.click()} title={up.received > 0 ? "Pick the same file: it goes on from where core has it" : "Pick the file to send"}>
          <UploadIcon />
          {up.received > 0 ? "Resume from a file" : "Pick the file"}
        </Button>
        <Button variant="outline" disabled={copying} onClick={() => void copyCurl()}
          title={up.received > 0 ? `A curl command going on from byte ${up.received}` : "A curl command with a signed link (24 hours at most)"}>
          <TerminalIcon />
          {up.received > 0 ? "Copy resume curl" : "Copy curl"}
        </Button>
      </div>
    </div>
  )
}

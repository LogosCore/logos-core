import { useContext, useState } from "react"
import { Loader2Icon, UploadIcon } from "lucide-react"
import { BuildFileControl } from "@/components/builds/build-file-control"
import { BuildFilesContext } from "@/components/builds/build-files-context"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { Draft } from "@/components/modules/management/field-values"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { cn } from "@/lib/utils"
import type { ConsoleCommand } from "./command-bar"
import { StreamFieldControl } from "./stream-field-control"
import { TaskFilesProvider } from "./task-files"
import { joinPath, samePathOn, type UploadTarget } from "./upload-target"

// A listing's Upload here (docs: contracts/task-results-and-actions.md,
// dir_listing View): the upload_file action's params as a form. The file is
// picked and uploaded to core at once; its destination starts as the
// folder, and becomes the folder and the file's name once one is picked,
// until the operator edits it; Overwrite is off, and the form says when the
// listing already has an entry of the name. Upload runs the command with
// the three.

export function UploadDialog({
  target,
  command,
  operationId,
  onOpenChange,
  onSubmit,
}: {
  /** Where to; null: closed. */
  target: UploadTarget | null
  command: ConsoleCommand | undefined
  operationId: string
  onOpenChange: (open: boolean) => void
  /** Runs the command with the action's params; stream, a file picked to
   *  stream once the task is queued. */
  onSubmit: (args: Record<string, unknown>, stream?: File) => Promise<unknown>
}) {
  return (
    <Dialog open={target !== null && command !== undefined} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {target && command && (
          <TaskFilesProvider key={`${target.folder}-${command.id}`} operationId={operationId}>
            <UploadForm target={target} command={command} onClose={() => onOpenChange(false)} onSubmit={onSubmit} />
          </TaskFilesProvider>
        )}
      </DialogContent>
    </Dialog>
  )
}

function UploadForm({
  target,
  command,
  onClose,
  onSubmit,
}: {
  target: UploadTarget
  command: ConsoleCommand
  onClose: () => void
  onSubmit: (args: Record<string, unknown>, stream?: File) => Promise<unknown>
}) {
  const files = useContext(BuildFilesContext)
  // A task file (upload_file), or a stream (upload_stream).
  const field = command.fields.find((f) => f.type === "file" || f.type === "stream")
  const streams = field?.type === "stream"
  const [file, setFile] = useState<Extract<Draft, { kind: "file" }>>({ kind: "file", id: null })
  const [stream, setStream] = useState<Extract<Draft, { kind: "stream" }>>({ kind: "stream", file: null, name: "" })
  const chosenName = streams ? (stream.file?.name ?? "") : (files?.files.find((f) => f.id === file.id)?.name ?? "")
  // The path as typed; until it is, the folder and the picked file's name.
  const [typed, setTyped] = useState<string | null>(null)
  const path = typed ?? (chosenName ? joinPath(target, chosenName) : target.folder)
  // A stream with no file picked goes with curl, and its path names it: not
  // the folder, nor one ending in a separator.
  const curl = streams && !stream.file
  const named = !curl || (path.trim() !== target.folder && !/[/\\]$/.test(path.trim()))
  const ready = (streams || chosenName !== "") && named
  const [overwrite, setOverwrite] = useState(false)
  const [pending, setPending] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  // On Windows a name in another case is the same file.
  const there = (target.names ?? []).some((n) => samePathOn(target, joinPath(target, n), path.trim()))

  const submit = async () => {
    if (!field || !ready || path.trim() === "" || pending) return
    setPending(true)
    setFailure(null)
    try {
      await onSubmit(
        streams
          ? { ...(stream.file ? { [field.key]: stream.file.name } : {}), path: path.trim(), overwrite }
          : { [field.key]: file.id, path: path.trim(), overwrite },
        stream.file ?? undefined,
      )
      onClose()
    } catch (err) {
      setFailure(errorMessage(err, "Could not upload it"))
      setPending(false)
    }
  }

  return (
    <form
      className="flex min-w-0 flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>Upload here</DialogTitle>
        <DialogDescription>
          {streams ? "A file of any size, streamed" : "A file from your computer"}, to{" "}
          <span className="font-mono break-all">{target.folder}</span>, with <span className="font-mono">{command.id}</span>.
        </DialogDescription>
      </DialogHeader>
      {field ? (
        <div className="flex flex-col gap-2">
          <Label htmlFor="upload-file">File</Label>
          {streams ? (
            <StreamFieldControl id="upload-file" draft={stream} disabled={pending} onChange={(d) => d.kind === "stream" && setStream(d)} />
          ) : (
            <BuildFileControl
              id="upload-file"
              field={field}
              draft={file}
              disabled={pending}
              onChange={(d) => d.kind === "file" && setFile(d)}
            />
          )}
        </div>
      ) : (
        <p className="text-sm text-destructive">{command.id} takes no file.</p>
      )}
      <div className="flex flex-col gap-2">
        <Label htmlFor="upload-path">Path on the host</Label>
        <Input
          id="upload-path"
          className="font-mono"
          value={path}
          disabled={pending}
          onChange={(e) => setTyped(e.target.value)}
        />
        <span className={cn("text-xs", named ? "text-muted-foreground" : "text-amber-700 dark:text-amber-400")}>
          {curl ? "No file picked: it goes with curl, so the path names the file. Add its name." : "A folder takes the file under its own name."}
        </span>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <Checkbox checked={overwrite} disabled={pending} onCheckedChange={(on) => setOverwrite(on === true)} />
        Overwrite a file already there
      </label>
      {there && !overwrite && (
        <p className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400">
          The folder already has {path.trim().slice(path.trim().lastIndexOf(target.sep) + 1)}: the upload fails unless Overwrite is ticked.
        </p>
      )}
      {failure && <p className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{failure}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending || !ready || path.trim() === ""}>
          {pending ? <Loader2Icon className="animate-spin" /> : <UploadIcon />}
          Upload
        </Button>
      </DialogFooter>
    </form>
  )
}

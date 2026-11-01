import { LoaderIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { FileView } from "@/components/minion-sessions/file-view"
import { useMinionTask } from "@/graphql/hooks/file-browser"

// A downloaded mark's file: the file core keeps of the download task, in
// the view its task shows it with (file-view.tsx). The view's tab row names
// the file, so the dialog has no header: its title is for screen readers,
// and its close sits at the row's end.

export function DownloadedFileDialog({ taskId, onClose }: { taskId: string | null; onClose: () => void }) {
  const q = useMinionTask(taskId)
  const file = q.data?.minionTask?.file ?? null
  const close = (
    <DialogClose render={<Button variant="ghost" size="icon-xs" title="Close" aria-label="Close" />}>
      <XIcon />
    </DialogClose>
  )
  return (
    <Dialog open={taskId !== null} onOpenChange={(open) => !open && onClose()}>
      {/* The view fills the dialog to its edges: the dialog is its frame. */}
      <DialogContent className="flex h-[80vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl" showCloseButton={false}>
        <DialogTitle className="sr-only">{file?.name ?? "Downloaded file"}</DialogTitle>
        <div className="flex min-h-0 flex-1 flex-col">
          {!file && <div className="flex justify-end border-b px-2 py-1">{close}</div>}
          {q.isLoading ? (
            <div className="flex flex-1 items-center justify-center text-muted-foreground">
              <LoaderIcon className="size-4 animate-spin" />
            </div>
          ) : file ? (
            <FileView key={file.id} file={file} end={close} />
          ) : (
            <p className="p-4 text-sm text-muted-foreground">
              {q.isError ? (q.error instanceof Error ? q.error.message : "Couldn't read the task.") : "Core no longer keeps this file."}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

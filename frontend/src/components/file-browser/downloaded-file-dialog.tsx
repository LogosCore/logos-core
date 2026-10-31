import { LoaderIcon } from "lucide-react"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { FileView } from "@/components/minion-sessions/file-view"
import { useMinionTask } from "@/graphql/hooks/file-browser"

// A downloaded mark's file: the file core keeps of the download task, in
// the view its task shows it with (file-view.tsx).

export function DownloadedFileDialog({ taskId, onClose }: { taskId: string | null; onClose: () => void }) {
  const q = useMinionTask(taskId)
  const file = q.data?.minionTask?.file ?? null
  return (
    <Dialog open={taskId !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex h-[80vh] flex-col gap-2 sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle className="truncate font-mono text-sm">{file?.name ?? "Downloaded file"}</DialogTitle>
        </DialogHeader>
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border">
          {q.isLoading ? (
            <div className="flex flex-1 items-center justify-center text-muted-foreground">
              <LoaderIcon className="size-4 animate-spin" />
            </div>
          ) : file ? (
            <FileView key={file.id} file={file} />
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

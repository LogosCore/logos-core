import { useMemo, useState, type ReactNode } from "react"
import { BuildFilesContext, type BuildFileRef, type BuildFilesContextValue } from "@/components/builds/build-files-context"
import { uploadTaskFile } from "@/lib/build-file-upload"

// The file a command's file param sends the minion (docs:
// contracts/task-results-and-actions.md, Task files): picked from the
// operator's computer, uploaded to core at once with its progress, and
// named by its id when the task is queued. The forms' file control
// (BuildFileControl) works from this as it does from a build's files; the
// files listed are the ones uploaded in this form, as core may drop a task
// file once its tasks end.

export function TaskFilesProvider({ operationId, children }: { operationId: string; children: ReactNode }) {
  const [files, setFiles] = useState<BuildFileRef[]>([])
  const value = useMemo<BuildFilesContextValue>(
    () => ({
      files,
      upload: async (file, onProgress) => {
        const f = await uploadTaskFile(file, operationId, { onProgress })
        const ref: BuildFileRef = { id: f.id, name: f.name, size: f.size, mediaType: f.contentType }
        setFiles((prev) => [...prev, ref])
        return ref
      },
    }),
    [files, operationId],
  )
  return <BuildFilesContext.Provider value={value}>{children}</BuildFilesContext.Provider>
}

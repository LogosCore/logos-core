import { createContext } from "react"
import type { FileLike } from "./build-values"

/** A build file of the operation, as the control lists it. */
export interface BuildFileRef extends FileLike {
  id: string
}

/**
 * What a file field needs from the form around it: the files it can pick
 * and a way to upload one. The build form provides the operation's build
 * files; a session's command forms, the task files uploaded in the form
 * (TaskFilesProvider).
 */
export interface BuildFilesContextValue {
  files: readonly BuildFileRef[]
  upload: (file: File, onProgress: (pct: number) => void) => Promise<BuildFileRef>
}

export const BuildFilesContext = createContext<BuildFilesContextValue | null>(null)

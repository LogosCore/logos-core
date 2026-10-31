import { createContext, useContext } from "react"

// The session work pane's tabs (session-workspace.tsx): a session's console,
// its id; or the file browser of a session's machine, "files:" and the id
// of the session it was opened from (docs: contracts/file-browser.md).

const FILES = "files:"

export type WorkspaceTab = { kind: "console"; sessionId: string } | { kind: "files"; sessionId: string }

/** The tab id of the file browser opened from a session. */
export function fileBrowserTabId(sessionId: string): string {
  return FILES + sessionId
}

/** What a tab id is. */
export function workspaceTab(id: string): WorkspaceTab {
  return id.startsWith(FILES) ? { kind: "files", sessionId: id.slice(FILES.length) } : { kind: "console", sessionId: id }
}

/** What the work pane's tabs can do from inside one. */
export interface WorkspaceActions {
  /** Opens (or brings to the front) a session's console. */
  openConsole: (sessionId: string) => void
  /** Opens a session's machine's file browser; at a folder, when given. */
  openFileBrowser: (sessionId: string, path?: string) => void
}

export const WorkspaceActionsContext = createContext<WorkspaceActions | null>(null)

/** The work pane's actions; null outside it. */
export function useWorkspaceActions(): WorkspaceActions | null {
  return useContext(WorkspaceActionsContext)
}

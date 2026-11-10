import { useNavigate } from "react-router"
import { useMinionSessionsLayout } from "@/graphql/hooks/minion-sessions"
import { openSessionTab } from "./use-session-tabs"

/**
 * Interact with a session from outside the Sessions page (a build's
 * targets): its tab opened in front in its operation's work pane, the pane
 * shown if it was parked, as Interact on the page does, and the Sessions
 * page opened on it. Any record modal closes: the page is where to work.
 */
export function useInteractWithSession(): (session: { id: string; operationId: string }) => void {
  const navigate = useNavigate()
  const { layout, isLoading, setLayout } = useMinionSessionsLayout()
  return (session) => {
    openSessionTab(session.operationId, session.id)
    // Only a layout read: one still loading would be the defaults, and saving
    // them would lose the operator's own.
    if (!isLoading && layout.paneParked) setLayout({ ...layout, paneParked: false })
    navigate("/sessions")
  }
}

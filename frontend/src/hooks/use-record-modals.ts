import { useLocation, useNavigate, useSearchParams } from "react-router"
import {
  openMove,
  recordModalOf,
  replaceMove,
  searchOf,
  withRecordModal,
  type RecordModal,
  type RecordModalMove,
} from "@/lib/record-modals"

/**
 * History state of a record modal's entry: inApp is set when the app opened
 * it over the page, so closing it goes back to that page's entry. A modal
 * opened from a link (or a reload of one) has no page entry to go back to,
 * so closing replaces its entry instead.
 */
interface RecordModalState {
  inApp?: boolean
}

function openedInApp(state: unknown): boolean {
  return typeof state === "object" && state !== null && (state as RecordModalState).inApp === true
}

/**
 * Opens and closes the record modals (lib/record-modals.ts). Opening one
 * over a page, or over another record (a session's build), adds a history
 * entry: closing it, or Back, returns to what it was opened from. Another
 * tab of the same record replaces its entry, and replace() moves a modal on
 * to what it made in its place.
 */
export function useRecordModals() {
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const current = recordModalOf(params)

  const go = (modal: RecordModal | null, move: RecordModalMove, extraState: object) =>
    navigate(
      { pathname: location.pathname, search: searchOf(withRecordModal(params, modal)), hash: location.hash },
      { replace: move.replace, state: { ...extraState, inApp: move.inApp } },
    )

  return {
    /** The modal the URL opens, if any. */
    current,
    /** Opens modal; extraState rides along in the history entry (an endpoint import's values). */
    open(modal: RecordModal, extraState: object = {}) {
      go(modal, openMove(current, modal, openedInApp(location.state)), extraState)
    },
    /** Puts modal in the open one's place: a form moving on to what it made. */
    replace(modal: RecordModal, extraState: object = {}) {
      go(modal, replaceMove(current, openedInApp(location.state)), extraState)
    },
    /** Closes the open modal; nothing when none is open. */
    close() {
      if (current === null) return
      if (openedInApp(location.state)) navigate(-1)
      else go(null, { replace: true, inApp: false }, {})
    },
  }
}

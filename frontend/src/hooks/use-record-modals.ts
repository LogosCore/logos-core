import { useLocation, useNavigate, useSearchParams } from "react-router"
import { recordModalOf, searchOf, withRecordModal, type RecordModal } from "@/lib/record-modals"

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
 * Opens and closes the build and endpoint modals (lib/record-modals.ts).
 * Opening one over a page adds a history entry, so Back closes it; opening
 * one from another replaces that one's entry, so a single Back still returns
 * to the page.
 */
export function useRecordModals() {
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const current = recordModalOf(params)

  const go = (modal: RecordModal | null, replace: boolean, state: object) =>
    navigate(
      { pathname: location.pathname, search: searchOf(withRecordModal(params, modal)), hash: location.hash },
      { replace, state },
    )

  return {
    /** The modal the URL opens, if any. */
    current,
    /** Opens modal; extraState rides along in the history entry (an endpoint import's values). */
    open(modal: RecordModal, extraState: object = {}) {
      const inApp = current === null || openedInApp(location.state)
      go(modal, current !== null, { ...extraState, inApp })
    },
    /** Closes the open modal. */
    close() {
      if (openedInApp(location.state)) navigate(-1)
      else go(null, true, {})
    },
  }
}

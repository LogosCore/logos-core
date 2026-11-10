// Builds and endpoints open in large modals over whatever page is showing,
// addressed by the URL's query, so a link to one opens it and Back closes it:
//
//   ?build=<id>[&buildTab=<tab>]               a build, on a tab of its own
//   ?newBuild=<factory>[&buildFrom=<id>][&buildSession=<id>]
//                                              a new build, from an earlier
//                                              one, persisting a session
//   ?endpoint=<id>                             an endpoint (its editor)
//   ?newEndpoint=<channel>[&endpointFrom=<id>] a new endpoint, from another
//   ?session=<id>[&sessionTab=<tab>]           a minion session
//
// The rest of the query belongs to the page underneath and is kept.

/** A build modal's tabs (?buildTab=). Without one, the modal picks by the build's state. */
export type BuildTab = "overview" | "json" | "pipeline" | "connections" | "commands" | "targets"

const BUILD_TABS: readonly BuildTab[] = ["overview", "json", "pipeline", "connections", "commands", "targets"]

/** A session modal's tabs (?sessionTab=); without one, its overview. */
export type SessionTab = "overview" | "connections" | "feedback" | "identity" | "json"

const SESSION_TABS: readonly SessionTab[] = ["overview", "connections", "feedback", "identity", "json"]

/** The modal the URL opens. */
export type RecordModal =
  | { kind: "build"; id: string; tab?: BuildTab }
  | { kind: "new-build"; factory: string; from: string | null; session?: string | null }
  | { kind: "endpoint"; id: string }
  | { kind: "new-endpoint"; channel: string; from: string | null }
  | { kind: "session"; id: string; tab?: SessionTab }

const BUILD = "build"
const BUILD_TAB = "buildTab"
const NEW_BUILD = "newBuild"
const BUILD_FROM = "buildFrom"
const BUILD_SESSION = "buildSession"
const ENDPOINT = "endpoint"
const NEW_ENDPOINT = "newEndpoint"
const ENDPOINT_FROM = "endpointFrom"
const SESSION = "session"
const SESSION_TAB = "sessionTab"

/** Every query key the record modals use. */
export const RECORD_MODAL_KEYS = [
  BUILD,
  BUILD_TAB,
  NEW_BUILD,
  BUILD_FROM,
  BUILD_SESSION,
  ENDPOINT,
  NEW_ENDPOINT,
  ENDPOINT_FROM,
  SESSION,
  SESSION_TAB,
]

/** The modal params name, or null; with several, the first of the order above. */
export function recordModalOf(params: URLSearchParams): RecordModal | null {
  const get = (key: string) => params.get(key)?.trim() || null
  const build = get(BUILD)
  if (build) {
    const tab = BUILD_TABS.find((t) => t === get(BUILD_TAB))
    return tab ? { kind: "build", id: build, tab } : { kind: "build", id: build }
  }
  const factory = get(NEW_BUILD)
  if (factory) {
    const session = get(BUILD_SESSION)
    return session ? { kind: "new-build", factory, from: get(BUILD_FROM), session } : { kind: "new-build", factory, from: get(BUILD_FROM) }
  }
  const endpoint = get(ENDPOINT)
  if (endpoint) return { kind: "endpoint", id: endpoint }
  const channel = get(NEW_ENDPOINT)
  if (channel) return { kind: "new-endpoint", channel, from: get(ENDPOINT_FROM) }
  const session = get(SESSION)
  if (session) {
    const tab = SESSION_TABS.find((t) => t === get(SESSION_TAB))
    return tab ? { kind: "session", id: session, tab } : { kind: "session", id: session }
  }
  return null
}

/** params with modal (or none) in place of whatever modal they named. */
export function withRecordModal(params: URLSearchParams, modal: RecordModal | null): URLSearchParams {
  const next = new URLSearchParams(params)
  for (const key of RECORD_MODAL_KEYS) next.delete(key)
  switch (modal?.kind) {
    case "build":
      next.set(BUILD, modal.id)
      if (modal.tab) next.set(BUILD_TAB, modal.tab)
      break
    case "new-build":
      next.set(NEW_BUILD, modal.factory)
      if (modal.from) next.set(BUILD_FROM, modal.from)
      if (modal.session) next.set(BUILD_SESSION, modal.session)
      break
    case "endpoint":
      next.set(ENDPOINT, modal.id)
      break
    case "new-endpoint":
      next.set(NEW_ENDPOINT, modal.channel)
      if (modal.from) next.set(ENDPOINT_FROM, modal.from)
      break
    case "session":
      next.set(SESSION, modal.id)
      if (modal.tab) next.set(SESSION_TAB, modal.tab)
      break
  }
  return next
}

/** The same string for the same modal; "" for none. */
export function recordModalKey(modal: RecordModal | null): string {
  return modal ? withRecordModal(new URLSearchParams(), modal).toString() : ""
}

/** Whether a and b show the same record: another tab of a build or a session is still it. */
export function sameRecord(a: RecordModal, b: RecordModal): boolean {
  const record = (m: RecordModal): RecordModal => (m.kind === "build" || m.kind === "session" ? { kind: m.kind, id: m.id } : m)
  return recordModalKey(record(a)) === recordModalKey(record(b))
}

/**
 * How a modal comes to the URL: replace, in place of the current entry, or
 * in an entry of its own; inApp, whether an entry of the app's is under it,
 * for closing it to go back to (a modal opened from a link has none).
 */
export interface RecordModalMove {
  replace: boolean
  inApp: boolean
}

/**
 * Opening next with current open (null: none), whose entry has one of the
 * app's under it when currentInApp. Over the page, or over another record
 * (a session's build, a build's target's session), an entry of its own:
 * closing it, or Back, returns to what it was opened from. Another tab of
 * the same record takes its entry's place, as it came.
 */
export function openMove(current: RecordModal | null, next: RecordModal, currentInApp: boolean): RecordModalMove {
  if (current !== null && sameRecord(current, next)) return { replace: true, inApp: currentInApp }
  return { replace: false, inApp: true }
}

/**
 * Moving the open modal on to next, in its place (a new build's form to
 * the build it started): closing next returns where closing current would.
 */
export function replaceMove(current: RecordModal | null, currentInApp: boolean): RecordModalMove {
  return current === null ? { replace: false, inApp: true } : { replace: true, inApp: currentInApp }
}

interface Place {
  pathname: string
  search: string
}

/**
 * Whether a navigation closes the modal from names, or puts another in its
 * place: what a form with unsaved changes asks about first. A change to the
 * page's own query keeps the modal, and goes through.
 */
export function leavesRecordModal(from: Place, to: Place): boolean {
  const modal = recordModalOf(new URLSearchParams(from.search))
  if (!modal) return false
  return (
    from.pathname !== to.pathname ||
    recordModalKey(modal) !== recordModalKey(recordModalOf(new URLSearchParams(to.search)))
  )
}

/** "?query" for params, or "" when there is none. */
export function searchOf(params: URLSearchParams): string {
  const s = params.toString()
  return s ? `?${s}` : ""
}

// Builds and endpoints open in large modals over whatever page is showing,
// addressed by the URL's query, so a link to one opens it and Back closes it:
//
//   ?build=<id>[&buildTab=<tab>]               a build, on a tab of its own
//   ?newBuild=<factory>[&buildFrom=<id>]       a new build, from an earlier one
//   ?endpoint=<id>                             an endpoint (its editor)
//   ?newEndpoint=<channel>[&endpointFrom=<id>] a new endpoint, from another
//
// The rest of the query belongs to the page underneath and is kept.

/** A build modal's tabs (?buildTab=). Without one, the modal picks by the build's state. */
export type BuildTab = "overview" | "json" | "pipeline"

const BUILD_TABS: readonly BuildTab[] = ["overview", "json", "pipeline"]

/** The modal the URL opens. */
export type RecordModal =
  | { kind: "build"; id: string; tab?: BuildTab }
  | { kind: "new-build"; factory: string; from: string | null }
  | { kind: "endpoint"; id: string }
  | { kind: "new-endpoint"; channel: string; from: string | null }

const BUILD = "build"
const BUILD_TAB = "buildTab"
const NEW_BUILD = "newBuild"
const BUILD_FROM = "buildFrom"
const ENDPOINT = "endpoint"
const NEW_ENDPOINT = "newEndpoint"
const ENDPOINT_FROM = "endpointFrom"

/** Every query key the record modals use. */
export const RECORD_MODAL_KEYS = [BUILD, BUILD_TAB, NEW_BUILD, BUILD_FROM, ENDPOINT, NEW_ENDPOINT, ENDPOINT_FROM]

/** The modal params name, or null; with several, the first of the order above. */
export function recordModalOf(params: URLSearchParams): RecordModal | null {
  const get = (key: string) => params.get(key)?.trim() || null
  const build = get(BUILD)
  if (build) {
    const tab = BUILD_TABS.find((t) => t === get(BUILD_TAB))
    return tab ? { kind: "build", id: build, tab } : { kind: "build", id: build }
  }
  const factory = get(NEW_BUILD)
  if (factory) return { kind: "new-build", factory, from: get(BUILD_FROM) }
  const endpoint = get(ENDPOINT)
  if (endpoint) return { kind: "endpoint", id: endpoint }
  const channel = get(NEW_ENDPOINT)
  if (channel) return { kind: "new-endpoint", channel, from: get(ENDPOINT_FROM) }
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
      break
    case "endpoint":
      next.set(ENDPOINT, modal.id)
      break
    case "new-endpoint":
      next.set(NEW_ENDPOINT, modal.channel)
      if (modal.from) next.set(ENDPOINT_FROM, modal.from)
      break
  }
  return next
}

/** The same string for the same modal; "" for none. */
export function recordModalKey(modal: RecordModal | null): string {
  return modal ? withRecordModal(new URLSearchParams(), modal).toString() : ""
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

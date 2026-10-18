import { useEffect, useRef, useState } from "react"
import { Doc as YDoc } from "yjs"
import { HocuspocusProvider } from "@hocuspocus/provider"
import { fetchCollabTicket, SchemaOutdatedError } from "@/lib/collab-ticket"

/** Build absolute WebSocket URL from the current page origin. */
function getWsUrl(): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${window.location.host}`
}

export type ConnectionStatus = "connecting" | "connected" | "disconnected"

// Work that still has to write into a room after its page has gone away. A
// drawing's image upload is the case: the element can only be published once
// the bytes are stored, and leaving the page inside the app unmounts the
// canvas long before that. Without a hold the teardown below destroyed the
// provider mid-upload and the image never reached the room.
const roomHolds = new WeakMap<YDoc, Set<Promise<void>>>()

// How long a held room may stay connected after its page left, so a hung
// upload cannot keep a socket open for the rest of the session.
const HOLD_TIMEOUT_MS = 120_000
// How long to wait for the server to acknowledge what was written last.
const UNSYNCED_TIMEOUT_MS = 5_000

/**
 * Keep a room's provider alive past its page's unmount until the returned
 * release function is called. The room still tears down afterwards, once its
 * last update has been acknowledged.
 */
export function holdRoom(doc: YDoc): () => void {
  let resolve!: () => void
  const held = new Promise<void>((r) => {
    resolve = r
  })
  let holds = roomHolds.get(doc)
  if (!holds) {
    holds = new Set()
    roomHolds.set(doc, holds)
  }
  holds.add(held)
  return () => {
    holds.delete(held)
    resolve()
  }
}

function timeout(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function waitForAcknowledged(provider: HocuspocusProvider): Promise<void> {
  if (!provider.hasUnsyncedChanges) return Promise.resolve()
  return Promise.race([
    new Promise<void>((resolve) => {
      const onUnsynced = () => {
        if (provider.hasUnsyncedChanges) return
        provider.off("unsyncedChanges", onUnsynced)
        resolve()
      }
      provider.on("unsyncedChanges", onUnsynced)
    }),
    timeout(UNSYNCED_TIMEOUT_MS),
  ])
}

interface UseHocuspocusReturn {
  ydoc: YDoc
  provider: HocuspocusProvider | null
  connectionStatus: ConnectionStatus
  isSynced: boolean
  isReady: boolean
  // True when the backend refused to connect because this client's editor
  // schema is older than the document's stored content (see SchemaOutdatedError).
  // The consumer should block editing and prompt the user to reload — no
  // WebSocket is opened in this state, so no content can be pruned.
  schemaOutdated: boolean
}

/**
 * Manages a Hocuspocus WebSocket connection for a single wiki document.
 *
 * Handles document switching internally — when `documentId` changes the
 * old Y.Doc and provider are destroyed and fresh ones are created. The
 * parent component does **not** need to use `key={documentId}`.
 */
export function useHocuspocus(documentId: string): UseHocuspocusReturn {
  const [ydoc, setYdoc] = useState<YDoc>(() => new YDoc())
  const [provider, setProvider] = useState<HocuspocusProvider | null>(null)
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>("connecting")
  const [isSynced, setIsSynced] = useState(false)
  const [isReady, setIsReady] = useState(false)
  const [schemaOutdated, setSchemaOutdated] = useState(false)

  // Tracks whether the provider has ever reached "connected" during this
  // document's session. Lets us distinguish "initial connect in progress"
  // (connecting) from "lost an established connection" (disconnected).
  const hasConnectedRef = useRef(false)

  // Reset session state during render when documentId changes — the provider
  // and ydoc are torn down and rebuilt by the effect below. Doing the reset
  // here (prev-value pattern, react.dev/.../storing-information-from-previous-renders)
  // avoids a setState-in-effect cascade.
  const [lastDocumentId, setLastDocumentId] = useState(documentId)
  if (lastDocumentId !== documentId) {
    setLastDocumentId(documentId)
    setConnectionStatus("connecting")
    setIsSynced(false)
    setIsReady(false)
    setSchemaOutdated(false)
  }

  // Provider + Y.Doc lifecycle: when documentId changes, tear down the
  // old provider and doc, then create fresh ones. The ydoc is stored in
  // state so consumers (WikiEditor) re-render and rebind automatically.
  // The setYdoc/setProvider calls below are flagged by react-hooks/set-state-in-effect,
  // but the React docs explicitly carve out this exception: "Effects are
  // intended to synchronize state ... with external systems." A YDoc and a
  // WebSocket provider are external resources whose handles must be reflected
  // in render state so consumers can rebind. Refs would break that rebind.
  useEffect(() => {
    hasConnectedRef.current = false
    let cancelled = false

    const doc = new YDoc()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setYdoc(doc)

    let hpProvider: HocuspocusProvider | null = null

    function onStatus({ status }: { status: string }) {
      if (status === "connected") {
        hasConnectedRef.current = true
        setConnectionStatus("connected")
      } else {
        setConnectionStatus(hasConnectedRef.current ? "disconnected" : "connecting")
      }
    }

    function onSynced({ state }: { state: boolean }) {
      setIsSynced(state)
      if (state && hasConnectedRef.current) {
        setIsReady(true)
      }
    }

    // Provider's token callback. The eagerly-fetched first ticket is reused on
    // the initial connect to avoid a duplicate request; reconnects re-fetch.
    // A SchemaOutdatedError on a reconnect (a deploy bumped the schema mid-
    // session) flips schemaOutdated so the editor surfaces the reload prompt.
    function makeTokenCallback(prefetched: string | null) {
      let used = false
      return async () => {
        if (prefetched && !used) {
          used = true
          return prefetched
        }
        try {
          return await fetchCollabTicket(documentId)
        } catch (err) {
          if (!cancelled && err instanceof SchemaOutdatedError) {
            setSchemaOutdated(true)
          }
          throw err
        }
      }
    }

    function buildProvider(firstTicket: string | null) {
      if (cancelled) return
      hpProvider = new HocuspocusProvider({
        url: `${getWsUrl()}/api/v1/ws/wiki/`,
        name: `wiki/${documentId}`,
        document: doc,
        preserveTrailingSlash: true,
        token: makeTokenCallback(firstTicket),
      })
      setProvider(hpProvider)
      hpProvider.on("status", onStatus)
      hpProvider.on("synced", onSynced)
    }

    // Eager pre-flight: fetch a ticket before opening the WebSocket so an
    // outdated client short-circuits to the reload prompt and never connects
    // (a connected stale editor could prune unknown nodes). Transient/other
    // failures still build the provider so its own retry/auth loop can recover.
    fetchCollabTicket(documentId)
      .then((ticket) => {
        if (cancelled) return
        buildProvider(ticket)
      })
      .catch((err) => {
        if (cancelled) return
        if (err instanceof SchemaOutdatedError) {
          setSchemaOutdated(true)
          return
        }
        buildProvider(null)
      })

    return () => {
      cancelled = true
      setProvider(null)
      const provider = hpProvider
      if (provider) {
        provider.off("status", onStatus)
        provider.off("synced", onSynced)
      }
      const destroy = () => {
        provider?.destroy()
        doc.destroy()
      }

      const holds = roomHolds.get(doc)
      if (!holds || holds.size === 0 || !provider) {
        destroy()
        return
      }
      void Promise.race([Promise.all(holds), timeout(HOLD_TIMEOUT_MS)])
        .then(() => waitForAcknowledged(provider))
        .finally(destroy)
    }
  }, [documentId])

  return {
    ydoc,
    provider,
    connectionStatus,
    isSynced,
    isReady,
    schemaOutdated,
  }
}

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { TypedDocumentString } from "@/graphql/gql/graphql"

// The registry opens subscriptions on the singleton graphql-ws client. Stub it
// so these tests exercise the registry's own lifecycle bookkeeping and nothing
// else — no socket, no auth, no reconnect logic. The stub keeps each
// subscription's sink, for a test to play the server, and the client's event
// listeners, for a test to play the socket.
interface FakeSink {
  next: (msg: { data?: unknown; errors?: unknown[] }) => void
  error: (err: unknown) => void
  complete: () => void
}
const subscribeCalls: string[] = []
const disposed: string[] = []
const sinks: Array<{ name: string; sink: FakeSink }> = []
const clientListeners = new Map<string, Array<() => void>>()
const fakeClient = {
  subscribe: (payload: { operationName?: string }, sink: FakeSink) => {
    const name = payload.operationName ?? "(anonymous)"
    subscribeCalls.push(name)
    sinks.push({ name, sink })
    return () => {
      disposed.push(name)
    }
  },
  on: (event: string, listener: () => void) => {
    clientListeners.set(event, [...(clientListeners.get(event) ?? []), listener])
    return () => {}
  },
}

vi.mock("@/lib/graphql-ws-client", () => ({
  getGraphQLWSClient: () => fakeClient,
}))

const {
  subscribe,
  pauseSubscriptions,
  resumeSubscriptions,
  onSubscriptionsResumed,
  REOPEN_MIN_MS,
  REOPEN_MAX_MS,
  SETTLE_MS,
} = await import("@/lib/subscription-registry")

/** The sink of the latest subscription opened under name. */
function lastSink(name: string): FakeSink {
  return [...sinks].reverse().find((s) => s.name === name)!.sink
}

/** The socket says event (closed, connected). */
function fire(event: string) {
  for (const listener of clientListeners.get(event) ?? []) listener()
}

// Stand-in for a codegen document. The real class, not a shape cast: the
// registry keys on object identity and reads the operation name back out of the
// string, so the test document has to behave like one. Written the way codegen
// writes them — leading newline, operation definition first.
function fakeDocument(name: string): TypedDocumentString<unknown, unknown> {
  return new TypedDocumentString(`
    subscription ${name} {
  ping
}
    `) as unknown as TypedDocumentString<unknown, unknown>
}

describe("subscription registry visibility gap", () => {
  // The registry is module state shared by every test here. A failed
  // assertion aborts before its own cleanup line would run, so cleanup is
  // collected and drained centrally — otherwise one failure cascades into
  // unrelated ones and hides which test actually broke.
  const cleanups: Array<() => void> = []

  function track<T extends () => void>(cleanup: T): T {
    cleanups.push(cleanup)
    return cleanup
  }

  beforeEach(() => {
    subscribeCalls.length = 0
    disposed.length = 0
  })

  afterEach(() => {
    while (cleanups.length > 0) cleanups.pop()!()
  })

  it("announces a resume after subscriptions were torn down", () => {
    const resumed = vi.fn()
    track(onSubscriptionsResumed(resumed))
    track(subscribe(fakeDocument("WikiDocumentChanged"), {}, () => {}))

    pauseSubscriptions()
    expect(disposed).toEqual(["WikiDocumentChanged"])
    expect(resumed).not.toHaveBeenCalled()

    resumeSubscriptions()
    expect(resumed).toHaveBeenCalledTimes(1)
  })

  it("re-opens the subscription it tore down", () => {
    track(subscribe(fakeDocument("TaskChanged"), {}, () => {}))
    expect(subscribeCalls).toEqual(["TaskChanged"])

    pauseSubscriptions()
    resumeSubscriptions()

    expect(subscribeCalls).toEqual(["TaskChanged", "TaskChanged"])
  })

  // An alt-tab with nothing subscribed loses no events, so catching up would
  // be pure cost. This is what keeps the blanket invalidation on the other end
  // from firing on every idle tab switch.
  it("stays quiet when nothing was subscribed", () => {
    const resumed = vi.fn()
    track(onSubscriptionsResumed(resumed))

    pauseSubscriptions()
    resumeSubscriptions()

    expect(resumed).not.toHaveBeenCalled()
  })

  // The gap is announced once per gap, not once per tab switch: a second
  // return with no intervening teardown has nothing new to report.
  it("announces once per gap", () => {
    const resumed = vi.fn()
    track(onSubscriptionsResumed(resumed))
    track(subscribe(fakeDocument("HostChanged"), {}, () => {}))

    pauseSubscriptions()
    resumeSubscriptions()
    resumeSubscriptions()

    expect(resumed).toHaveBeenCalledTimes(1)
  })

  it("stops notifying after the listener unregisters", () => {
    const resumed = vi.fn()
    onSubscriptionsResumed(resumed)()
    track(subscribe(fakeDocument("CredentialChanged"), {}, () => {}))

    pauseSubscriptions()
    resumeSubscriptions()

    expect(resumed).not.toHaveBeenCalled()
  })
})

// The other gaps: the server ending a subscription (a refusal, or core's bus
// dropping events for it), and the socket dropping under open ones.
describe("subscription registry: subscriptions the server ends, and the socket", () => {
  const cleanups: Array<() => void> = []

  function track<T extends () => void>(cleanup: T): T {
    cleanups.push(cleanup)
    return cleanup
  }

  beforeEach(() => {
    vi.useFakeTimers()
    subscribeCalls.length = 0
    disposed.length = 0
    sinks.length = 0
  })

  afterEach(() => {
    while (cleanups.length > 0) cleanups.pop()!()
    // An announcement still waiting goes, before the next test listens.
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
  })

  it("opens one the server completed again after a wait, then announces the gap", () => {
    const resumed = vi.fn()
    track(onSubscriptionsResumed(resumed))
    track(subscribe(fakeDocument("SessionChanged"), {}, () => {}))

    lastSink("SessionChanged").complete()
    vi.advanceTimersByTime(REOPEN_MIN_MS - 1)
    expect(subscribeCalls).toEqual(["SessionChanged"])
    vi.advanceTimersByTime(1)
    expect(subscribeCalls).toEqual(["SessionChanged", "SessionChanged"])

    // Announced once the server has had time to take it.
    expect(resumed).not.toHaveBeenCalled()
    vi.advanceTimersByTime(SETTLE_MS)
    expect(resumed).toHaveBeenCalledTimes(1)
  })

  it("waits longer each time the server ends it again soon after", () => {
    track(subscribe(fakeDocument("TaskChanged"), {}, () => {}))
    lastSink("TaskChanged").complete()
    vi.advanceTimersByTime(REOPEN_MIN_MS)
    expect(subscribeCalls).toHaveLength(2)

    lastSink("TaskChanged").complete()
    vi.advanceTimersByTime(REOPEN_MIN_MS)
    expect(subscribeCalls).toHaveLength(2)
    vi.advanceTimersByTime(REOPEN_MIN_MS)
    expect(subscribeCalls).toHaveLength(3)
  })

  it("tells a refusal to its error listeners, not to its listeners", () => {
    const logged = vi.spyOn(console, "error").mockImplementation(() => {})
    const listener = vi.fn()
    const onError = vi.fn()
    track(subscribe(fakeDocument("ModuleLogs"), {}, listener, onError))

    // How gqlgen refuses one: a result with errors and no data, then complete.
    lastSink("ModuleLogs").next({ errors: [{ message: "module logs are not available" }] })
    lastSink("ModuleLogs").complete()

    expect(onError).toHaveBeenCalledWith("module logs are not available")
    expect(listener).not.toHaveBeenCalled()
    expect(logged).toHaveBeenCalledTimes(1)
    logged.mockRestore()
  })

  it("does not open again one it closed itself, whose complete comes late", () => {
    track(subscribe(fakeDocument("HostChanged"), {}, () => {}))
    const first = lastSink("HostChanged")

    pauseSubscriptions()
    // graphql-ws completes a subscription disposed here, a moment later.
    first.complete()
    vi.advanceTimersByTime(REOPEN_MAX_MS)
    expect(subscribeCalls).toEqual(["HostChanged"])

    resumeSubscriptions()
    expect(subscribeCalls).toEqual(["HostChanged", "HostChanged"])
  })

  it("calls off the reopen when the last listener leaves", () => {
    const unsubscribe = subscribe(fakeDocument("CredentialChanged"), {}, () => {})
    lastSink("CredentialChanged").complete()
    unsubscribe()
    vi.advanceTimersByTime(REOPEN_MAX_MS)
    expect(subscribeCalls).toEqual(["CredentialChanged"])
  })

  it("announces a gap once the socket is back after closing under open subscriptions", () => {
    const resumed = vi.fn()
    track(onSubscriptionsResumed(resumed))
    track(subscribe(fakeDocument("SessionChanged"), {}, () => {}))

    fire("closed")
    fire("closed") // each failed reconnect closes again
    vi.advanceTimersByTime(SETTLE_MS)
    expect(resumed).not.toHaveBeenCalled()

    fire("connected")
    vi.advanceTimersByTime(SETTLE_MS - 1)
    expect(resumed).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(resumed).toHaveBeenCalledTimes(1)
  })

  it("stays quiet when the socket closes with nothing open", () => {
    const resumed = vi.fn()
    track(onSubscriptionsResumed(resumed))
    fire("closed")
    fire("connected")
    vi.advanceTimersByTime(SETTLE_MS)
    expect(resumed).not.toHaveBeenCalled()
  })
})

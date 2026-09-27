import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { TypedDocumentString } from "@/graphql/gql/graphql"

// The registry opens subscriptions on the singleton graphql-ws client. Stub it
// so these tests exercise the registry's own lifecycle bookkeeping and nothing
// else — no socket, no auth, no reconnect logic.
const subscribeCalls: string[] = []
const disposed: string[] = []

vi.mock("@/lib/graphql-ws-client", () => ({
  getGraphQLWSClient: () => ({
    subscribe: (payload: { operationName?: string }) => {
      const name = payload.operationName ?? "(anonymous)"
      subscribeCalls.push(name)
      return () => {
        disposed.push(name)
      }
    },
  }),
}))

const {
  subscribe,
  pauseSubscriptions,
  resumeSubscriptions,
  onSubscriptionsResumed,
} = await import("@/lib/subscription-registry")

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

import { afterEach, beforeEach, describe, expect, test, vi } from "vitest"
import { useConnectivityStore } from "@/stores/connectivity"
import { apiFetch } from "./api-client"

// A fetch that fails marks the backend unreachable, and its recovery
// re-runs App's auth check, which remounts the whole app: a fetch its caller
// aborted (a component that unmounted, StrictMode's second effect run) must
// not, or every such abort looks like a page reload.

/** A fetch that never answers, and rejects as the browser does once its signal aborts. */
function hangingFetch() {
  return vi.fn(
    (_url: string, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("The operation was aborted.", "AbortError")))
      }),
  )
}

describe("apiFetch and reachability", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    useConnectivityStore.setState({ reachable: true })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  test("a fetch its caller aborted leaves the backend reachable", async () => {
    vi.stubGlobal("fetch", hangingFetch())
    const ac = new AbortController()
    const got = apiFetch("/recordings/r1", { timeoutMs: 0, signal: ac.signal })
    ac.abort()
    await expect(got).rejects.toThrow(/aborted/)
    expect(useConnectivityStore.getState().reachable).toBe(true)
  })

  test("a network failure marks it unreachable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("Failed to fetch"))),
    )
    await expect(apiFetch("/recordings/r1")).rejects.toThrow(/Failed to fetch/)
    expect(useConnectivityStore.getState().reachable).toBe(false)
  })
})

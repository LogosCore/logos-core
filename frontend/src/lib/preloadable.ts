import { useEffect, useState } from "react"

/**
 * Code fetched with import() when it is first wanted, or ahead of that with
 * load(), and at hand synchronously from then on.
 *
 * What React.lazy does not give: a lazy component suspends on its first
 * render even when its code has already been fetched, and React holds back
 * a Suspense boundary's content until 300 ms after its fallback showed
 * (FALLBACK_THROTTLE_MS in react-dom). Fetching early then saves nothing,
 * and a dialog's first open lags. useLoaded() renders loaded code at once.
 */
export interface Preloadable<T> {
  /** Fetches the code: once, or again after a failure. */
  load(): Promise<T>
  /** The module once loaded, else null. */
  current(): T | null
}

export function preloadable<T>(importer: () => Promise<T>): Preloadable<T> {
  let loaded: T | null = null
  let loading: Promise<T> | null = null
  return {
    load() {
      loading ??= importer().then(
        (m) => (loaded = m),
        (err: unknown) => {
          loading = null
          throw err
        },
      )
      return loading
    },
    current: () => loaded,
  }
}

/**
 * code's module: at once when it is loaded, else null while it loads, then
 * a render with it. A failed load is thrown, for the error boundary.
 */
export function useLoaded<T>(code: Preloadable<T>): T | null {
  const [loaded, setLoaded] = useState(code.current)
  const [error, setError] = useState<Error | null>(null)
  useEffect(() => {
    if (loaded) return
    let live = true
    code.load().then(
      (m) => {
        if (live) setLoaded(m)
      },
      (err: unknown) => {
        if (live) setError(err instanceof Error ? err : new Error(String(err)))
      },
    )
    return () => {
      live = false
    }
  }, [code, loaded])
  if (error) throw error
  return loaded
}

/**
 * Runs fn once the browser is idle (5 s at the latest), or after 2 s where it
 * cannot say; returns a cancel. The timings deferred-overlay.tsx warms with.
 */
export function whenIdle(fn: () => void): () => void {
  if (typeof window.requestIdleCallback === "function") {
    const handle = window.requestIdleCallback(fn, { timeout: 5_000 })
    return () => window.cancelIdleCallback(handle)
  }
  const timer = window.setTimeout(fn, 2_000)
  return () => window.clearTimeout(timer)
}

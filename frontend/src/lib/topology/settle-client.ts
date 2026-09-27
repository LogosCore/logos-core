// Main-thread side of the settle worker.
//
// One worker for the whole session, created on first use and kept warm: a lens
// switch or a data refresh re-settles, and spinning up a module worker per
// rebuild would cost more than it saves. Requests are tagged so a reply that
// arrives after the caller has moved on can be dropped instead of painted.
//
// Every failure mode degrades to "settle on the main thread" rather than to a
// broken map: no Worker constructor (jsdom under vitest), a worker that throws
// while starting, or a settle that throws inside it. The caller treats a null
// result as "do it inline" — which is exactly the behavior this replaced, so
// the fallback is a known-good path rather than an untested one.

import type { SettleInput, SettledPosition } from "@/lib/topology/settle"
import type { SettleRequest, SettleResponse } from "@/lib/topology/settle.worker"

type Pending = {
  resolve: (positions: SettledPosition[] | null) => void
}

let worker: Worker | null = null
let unavailable = false
let nextId = 1
const pending = new Map<number, Pending>()

function settleAllPending(value: SettledPosition[] | null) {
  for (const p of pending.values()) p.resolve(value)
  pending.clear()
}

function getWorker(): Worker | null {
  if (unavailable) return null
  if (worker) return worker
  if (typeof Worker === "undefined") {
    unavailable = true
    return null
  }
  try {
    worker = new Worker(
      new URL("@/lib/topology/settle.worker.ts", import.meta.url),
      { type: "module" },
    )
  } catch {
    unavailable = true
    return null
  }

  worker.onmessage = (event: MessageEvent<SettleResponse>) => {
    const reply = event.data
    const entry = pending.get(reply.id)
    if (!entry) return // superseded; the caller stopped caring
    pending.delete(reply.id)
    entry.resolve(reply.ok ? reply.positions : null)
  }

  // A worker-level error (bad module, OOM) never delivers per-request replies,
  // so everything in flight has to be released or those callers hang forever.
  // The worker is dropped and the process falls back to inline settling: a
  // second failing spin-up per rebuild would be pure cost.
  worker.onerror = () => {
    unavailable = true
    worker?.terminate()
    worker = null
    settleAllPending(null)
  }

  return worker
}

/**
 * Settles in the worker. Resolves with positions, or null when the caller
 * should settle inline instead.
 */
export function requestSettle(
  input: SettleInput,
): Promise<SettledPosition[] | null> {
  const w = getWorker()
  if (!w) return Promise.resolve(null)

  const id = nextId++
  return new Promise<SettledPosition[] | null>((resolve) => {
    pending.set(id, { resolve })
    const request: SettleRequest = { id, input }
    try {
      w.postMessage(request)
    } catch {
      // A non-cloneable payload would be a programming error in the input
      // shape, not a runtime condition — fail to the inline path rather than
      // leaving the promise unresolved.
      pending.delete(id)
      resolve(null)
    }
  })
}

// There is deliberately no "cancel" here. A settle already running in the worker
// cannot be interrupted, and resolving its promise early would hand the caller a
// null it would read as "settle inline" — re-doing on the main thread the work
// the worker is at that moment finishing. Callers drop results they no longer
// want with their own guard (see useTopologySimulation), which costs one
// comparison and never duplicates work.

// Worker that runs the topology force settle off the main thread.
//
// The settle is 300–410 d3-force ticks plus a crossing-reduction pass over the
// whole graph. On the main thread that is an uninterruptible freeze whose length
// scales with the operation's size — the dense identities lens is the bad case.
// d3-force touches no DOM, so it runs here unchanged.
//
// Requests carry a monotonic id and replies echo it: a lens switch or a data
// refresh can arrive while an earlier settle is still running, and the caller
// has to be able to drop the stale answer rather than paint it. Messages are
// plain objects so nothing depends on structured-clone handling anything exotic.

import { settleTopology, type SettleInput, type SettledPosition } from "@/lib/topology/settle"

export interface SettleRequest {
  id: number
  input: SettleInput
}

export type SettleResponse =
  | { id: number; ok: true; positions: SettledPosition[] }
  | { id: number; ok: false; error: string }

self.onmessage = (event: MessageEvent<SettleRequest>) => {
  const { id, input } = event.data
  try {
    const positions = settleTopology(input)
    const reply: SettleResponse = { id, ok: true, positions }
    self.postMessage(reply)
  } catch (error) {
    // A thrown settle must still answer, or the caller waits forever on a
    // request id that will never come back. The caller falls back to running
    // the settle inline.
    const reply: SettleResponse = {
      id,
      ok: false,
      error: error instanceof Error ? error.message : String(error),
    }
    self.postMessage(reply)
  }
}

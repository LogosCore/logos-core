// Rate limit for the auth re-check App runs when the backend comes back.
//
// A real outage recovers once. Recoveries arriving back to back mean the
// re-check itself is what keeps knocking reachability over (a request it
// makes fails in a way that is reported as an outage, the /status poll then
// succeeds immediately, and the cycle repeats). After `maxBurst` recoveries
// with less than `quietMs` between them, further ones are ignored until the
// connection has stayed up for `quietMs`.
export class RecoveryRecheckGate {
  private readonly maxBurst: number
  private readonly quietMs: number
  private burst = 0
  private last = Number.NEGATIVE_INFINITY

  constructor(maxBurst = 3, quietMs = 60_000) {
    this.maxBurst = maxBurst
    this.quietMs = quietMs
  }

  shouldRecheck(now: number): boolean {
    this.burst = now - this.last < this.quietMs ? this.burst + 1 : 1
    this.last = now
    return this.burst <= this.maxBurst
  }
}

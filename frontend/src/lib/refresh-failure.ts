// How a failed POST /login/refresh should be treated.
//
//   - "rejected":    the backend said the refresh token is no good. End the
//                    session.
//   - "unreachable": no answer from the backend itself — a gateway status
//                    from nginx or a load balancer while it restarts. Keep the
//                    session and let the connectivity poll wait for it.
//   - "failed":      the backend answered and failed (a plain 500). It is
//                    reachable, so it must not be reported as unreachable:
//                    the /status poll would succeed at once, App would re-run
//                    checkAuth on that "recovery", the refresh would fail the
//                    same way, and the login page flashed "Backend
//                    unreachable" forever. Keep the session and give up on
//                    this refresh.
export type RefreshFailure = "rejected" | "unreachable" | "failed"

const GATEWAY_STATUSES = new Set([502, 503, 504])

export function classifyRefreshFailure(status: number): RefreshFailure {
  if (status === 401 || status === 403) return "rejected"
  if (GATEWAY_STATUSES.has(status)) return "unreachable"
  return "failed"
}

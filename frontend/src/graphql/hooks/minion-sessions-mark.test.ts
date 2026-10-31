import { describe, expect, test } from "vitest"
import { QueryClient } from "@tanstack/react-query"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { applyMinionSessionMark, minionSessionKeys } from "./minion-sessions"

describe("a mark set on a session", () => {
  // A mark is the caller's, not in the session's rev: the mutation's copy of
  // the session can be older than the held one (a check-in came first), and
  // its mark is the newest all the same.
  test("takes the mark, and leaves the newer check-in", () => {
    const qc = new QueryClient()
    const held = { id: "s1", rev: 9, lastSeenAt: "newer", mark: null } as unknown as MinionSessionFieldsFragment
    qc.setQueryData(minionSessionKeys.detail("s1"), { minionSession: held })
    const answer = { id: "s1", rev: 7, lastSeenAt: "older", mark: { color: "gold" } } as unknown as MinionSessionFieldsFragment
    applyMinionSessionMark(qc, answer)
    const after = qc.getQueryData<{ minionSession: MinionSessionFieldsFragment }>(minionSessionKeys.detail("s1"))!.minionSession
    expect(after).toMatchObject({ rev: 9, lastSeenAt: "newer", mark: { color: "gold" } })
  })
})

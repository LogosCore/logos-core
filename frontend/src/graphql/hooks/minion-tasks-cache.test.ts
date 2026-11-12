import { describe, expect, test } from "vitest"
import { QueryClient } from "@tanstack/react-query"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { minionTaskKeys, NO_TASK_FILTER, upsertMinionTask } from "./minion-tasks"

const task = (rev: number, state: string): MinionTaskFieldsFragment =>
  ({ id: "t1", rev, sessionId: "s1", state, parts: rev > 2 ? 1 : 0 }) as MinionTaskFieldsFragment

describe("a session's tasks, kept live", () => {
  // The bug: a fast minion's updates came before the queue mutation's
  // answer, which then took the task back to queued for good.
  test("the queue mutation's answer after the live updates does not take the task back", () => {
    const qc = new QueryClient()
    qc.setQueryData(minionTaskKeys.list("s1", NO_TASK_FILTER), {
      pageParams: [null],
      pages: [{ minionTasks: { tasks: [], before: null } }],
    })
    upsertMinionTask(qc, task(1, "QUEUED")) // the live update of its queueing
    upsertMinionTask(qc, task(5, "DONE")) // sent, running, done: live
    upsertMinionTask(qc, task(1, "QUEUED")) // the mutation's answer, late
    const cached = qc.getQueryData<{ pages: { minionTasks: { tasks: MinionTaskFieldsFragment[] } }[] }>(minionTaskKeys.list("s1", NO_TASK_FILTER))
    expect(cached!.pages[0].minionTasks.tasks).toHaveLength(1)
    expect(cached!.pages[0].minionTasks.tasks[0]).toMatchObject({ state: "DONE", rev: 5 })
  })
})

import { describe, expect, test } from "vitest"
import { QueryClient, type InfiniteData, type QueryKey } from "@tanstack/react-query"
import type { MinionTaskFieldsFragment, MinionTasksQuery } from "@/graphql/gql/graphql"
import { mayJoin, minionTaskKeys, NO_TASK_FILTER, upsertMinionTask, type MinionTaskFilter } from "./minion-tasks"

const task = (id: string, rev: number, importantAt: string | null = null): MinionTaskFieldsFragment =>
  ({ id, rev, sessionId: "s1", state: "QUEUED", importantAt, parts: 0 }) as MinionTaskFieldsFragment

const pages = (...tasks: MinionTaskFieldsFragment[]): InfiniteData<MinionTasksQuery, string | null> => ({
  pageParams: [null],
  pages: [{ minionTasks: { tasks, before: null } }],
})

const ALL = minionTaskKeys.list("s1", NO_TASK_FILTER)
const IMPORTANT = minionTaskKeys.list("s1", { search: "", important: true })
const SEARCH = minionTaskKeys.list("s1", { search: "whoami", important: false })
const COUNT = minionTaskKeys.importantCount("s1")

// The session's lists as the console holds them: the whole one, the
// important ones, a search; and the count of important tasks.
function seeded() {
  const qc = new QueryClient()
  qc.setQueryData(ALL, pages(task("b", 2, "2026-11-13T10:00:00Z"), task("a", 3)))
  qc.setQueryData(IMPORTANT, pages(task("b", 2, "2026-11-13T10:00:00Z")))
  qc.setQueryData(SEARCH, pages(task("a", 3)))
  qc.setQueryData(COUNT, { minionTaskImportantCount: 1 })
  return qc
}

const ids = (qc: QueryClient, key: QueryKey) =>
  qc.getQueryData<InfiniteData<MinionTasksQuery>>(key)!.pages.flatMap((p) => p.minionTasks.tasks.map((t) => t.id))
const stale = (qc: QueryClient, key: QueryKey) => qc.getQueryState(key)?.isInvalidated ?? false

describe("a session's lists, kept live", () => {
  test("a task's update reaches every list that holds it", () => {
    const qc = seeded()
    upsertMinionTask(qc, { ...task("a", 4), state: "DONE" })
    for (const key of [ALL, SEARCH]) {
      expect(qc.getQueryData<InfiniteData<MinionTasksQuery>>(key)!.pages[0].minionTasks.tasks.find((t) => t.id === "a")?.state).toBe("DONE")
    }
    expect(stale(qc, IMPORTANT) || stale(qc, COUNT)).toBe(false)
  })

  test("a new task tops the whole list; a search reads its lines again, the important ones do not", () => {
    const qc = seeded()
    upsertMinionTask(qc, task("c", 1))
    expect(ids(qc, ALL)).toEqual(["c", "b", "a"])
    expect(ids(qc, SEARCH)).toEqual(["a"])
    expect(stale(qc, SEARCH)).toBe(true)
    expect(stale(qc, IMPORTANT)).toBe(false)
  })

  test("a task marked elsewhere: the important ones are read again, and their count", () => {
    const qc = seeded()
    upsertMinionTask(qc, task("a", 4, "2026-11-13T10:05:00Z"))
    expect(stale(qc, IMPORTANT)).toBe(true)
    expect(stale(qc, COUNT)).toBe(true)
    // The search does not read it again: its line did not change.
    expect(stale(qc, SEARCH)).toBe(false)
  })

  test("an unmarked task stays among the important ones, unstarred, until they are read again", () => {
    const qc = seeded()
    upsertMinionTask(qc, task("b", 3))
    expect(ids(qc, IMPORTANT)).toEqual(["b"])
    expect(qc.getQueryData<InfiniteData<MinionTasksQuery>>(IMPORTANT)!.pages[0].minionTasks.tasks[0].importantAt).toBeNull()
    expect(stale(qc, COUNT)).toBe(true)
  })

  test("an older copy changes nothing, its mark included", () => {
    const qc = seeded()
    upsertMinionTask(qc, task("b", 1))
    expect(qc.getQueryData<InfiniteData<MinionTasksQuery>>(ALL)!.pages[0].minionTasks.tasks[0].importantAt).not.toBeNull()
    expect(stale(qc, COUNT)).toBe(false)
  })
})

describe("mayJoin", () => {
  const important: MinionTaskFilter = { search: "", important: true }
  const search: MinionTaskFilter = { search: "x", important: false }
  test("the important ones take a marked task; a search a new one", () => {
    expect(mayJoin(important, task("a", 5, "2026-11-13T10:00:00Z"))).toBe(true)
    expect(mayJoin(important, task("a", 1))).toBe(false)
    expect(mayJoin(search, task("a", 1))).toBe(true)
    expect(mayJoin(search, task("a", 2))).toBe(false)
    expect(mayJoin({ search: "x", important: true }, task("a", 1))).toBe(false)
  })
})

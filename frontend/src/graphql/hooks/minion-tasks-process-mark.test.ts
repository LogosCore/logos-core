import { expect, test } from "vitest"
import { QueryClient, type InfiniteData } from "@tanstack/react-query"
import type { MinionTaskProcessesQuery } from "@/graphql/gql/graphql"
import { applyProcessMark, minionTaskKeys } from "./minion-tasks"

const list = (...names: string[]) =>
  ({
    pages: [
      {
        minionTaskProcesses: {
          edges: names.map((name, i) => ({ cursor: String(i + 1), node: { pid: i + 1, name, mark: null } })),
        },
      },
    ],
    pageParams: [undefined],
  }) as unknown as InfiniteData<MinionTaskProcessesQuery>

const marks = (qc: QueryClient, key: readonly unknown[]) =>
  qc.getQueryData<InfiniteData<MinionTaskProcessesQuery>>(key)!.pages[0].minionTaskProcesses.edges.map((e) => [e.node.name, e.node.mark?.color ?? null])

// A mark set on a name reaches every process of it, whatever its case, in
// every list cached (another task's too), and no other; cleared, none.
test("a mark set on a process name, in every list", () => {
  const qc = new QueryClient()
  const a = [...minionTaskKeys.processes("t1", { search: "", order: "TREE", desc: false, fold: "NONE", flip: [] }), "1:DONE"]
  const b = [...minionTaskKeys.processes("t2", { search: "fire", order: "PID", desc: false, fold: "NONE", flip: [] }), "2:DONE"]
  qc.setQueryData(a, list("System", "firefox.exe"))
  qc.setQueryData(b, list("Firefox.EXE", "sshd"))
  applyProcessMark(qc, " FIREFOX.exe ", { color: "red-strong", icon: null, emoji: "🦊", builtin: false })
  expect(marks(qc, a)).toEqual([["System", null], ["firefox.exe", "red-strong"]])
  expect(marks(qc, b)).toEqual([["Firefox.EXE", "red-strong"], ["sshd", null]])
  applyProcessMark(qc, "firefox.exe", null)
  expect(marks(qc, b)).toEqual([["Firefox.EXE", null], ["sshd", null]])
})

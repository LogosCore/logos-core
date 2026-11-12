import type { MinionTaskFilter } from "@/graphql/hooks/minion-tasks"

// What a session's task list says when nothing is in it, by what narrowed
// it (core's search of the tasks' lines, the important ones alone).

/** The list's text when it holds no task. */
export function emptyListText(filter: MinionTaskFilter): string {
  const search = filter.search.trim()
  if (search !== "" && filter.important) return `No important tasks match “${search}”.`
  if (search !== "") return `No tasks match “${search}”.`
  if (filter.important) return "No important tasks."
  return "No tasks yet."
}

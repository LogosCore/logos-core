import { useMemo } from "react"
import { compareFormOrder } from "@/components/modules/management/field-values"
import { useMinionSessionCommands } from "@/graphql/hooks/minion-tasks"
import type { ConsoleCommand } from "./command-bar"

/** A session's commands as the console and its views run them. */
export function useConsoleCommands(sessionId: string) {
  const q = useMinionSessionCommands(sessionId)
  const commands = useMemo<ConsoleCommand[]>(
    () =>
      (q.data?.minionSessionCommands ?? []).map((c) => ({
        id: c.id,
        label: c.label,
        description: c.description,
        result: c.result,
        action: c.action,
        kinds: c.kinds,
        fields: c.params,
        // In the form's order: the order words fill them in.
        params: [...c.params].sort(compareFormOrder).map((p) => ({
          key: p.key,
          type: p.type,
          label: p.label,
          required: p.required,
          default: p.default,
          format: p.format,
          when: p.when,
          options: p.options?.map((o) => o.value) ?? null,
        })),
      })),
    [q.data],
  )
  return { commands, error: q.error, isLoading: q.isLoading }
}

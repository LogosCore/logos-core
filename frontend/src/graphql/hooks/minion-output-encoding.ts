import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { MinionTaskOutputDecodingsDocument, SetMinionSessionOutputEncodingDocument } from "@/graphql/gql/graphql"
import { applyMinionSessionEvent } from "./minion-sessions"
import { minionTaskKeys } from "./minion-tasks"

// The Encodings table's read and its one write, apart from the session and
// task hooks, which much of the app imports: only the table needs these.

/**
 * A piece of a task's output read in every encoding core knows, while
 * enabled (the Encodings table is open), again when version changes (more
 * output came).
 */
export function useMinionTaskOutputDecodings(id: string, version: string, enabled: boolean) {
  return useQuery({
    queryKey: [...minionTaskKeys.all, "outputDecodings", id, version],
    queryFn: () => graphqlClient(MinionTaskOutputDecodingsDocument, { id }),
    enabled,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[2] === id ? prev : undefined),
  })
}

/** Sets what a session's tasks' output is read as, for the whole operation; null goes back to UTF-8. */
export function useSetMinionSessionOutputEncoding() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (vars: { id: string; encoding: string | null }) => graphqlClient(SetMinionSessionOutputEncodingDocument, vars),
    onSuccess: (data) => applyMinionSessionEvent(queryClient, "UPDATED", data.setMinionSessionOutputEncoding),
  })
}

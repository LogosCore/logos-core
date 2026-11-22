import { useQuery } from "@tanstack/react-query"
import { graphqlClient } from "@/lib/graphql-client"
import { StorageWarningsDocument } from "@/graphql/gql/graphql"

// The sidebar's mark on Storage: the store's warnings alone, read every
// minute. Apart from the page's hooks (storage.ts), so the app shell carries
// this one query, not the page's. Its key is under the page's ("storage"):
// an action there reads it again too.

export const storageWarningsKey = ["storage", "warnings"] as const

/** The store's warnings: only where the user may read the store (enabled). */
export function useStorageWarnings(enabled: boolean) {
  return useQuery({
    queryKey: storageWarningsKey,
    queryFn: () => graphqlClient(StorageWarningsDocument),
    refetchInterval: 60_000,
    enabled,
  })
}

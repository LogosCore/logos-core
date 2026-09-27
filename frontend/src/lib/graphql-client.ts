import type { TypedDocumentString } from "@/graphql/gql/graphql"
import { apiFetch } from "@/services/api-client"

// Documents arrive as query strings, not ASTs — codegen runs with
// `documentMode: "string"` (see codegen.ts).
//
// Before, every generated operation was an AST object literal and this function
// called graphql-js `print` on it per request. That cost twice: graphql-js's
// printer and language module sat in the first-paint chunk, and the AST walk ran
// again on each call, allocating the same string the last call already built.
// subscription-registry.ts had noticed the second half and memoized around it;
// this path never did.
//
// A string document is what the wire wants, so there is nothing left to print
// and nothing to cache.

export interface GraphQLError {
  message: string
  path?: string[]
  extensions?: Record<string, unknown>
}

export class GraphQLRequestError extends Error {
  errors: GraphQLError[]

  constructor(errors: GraphQLError[]) {
    super(errors.map((e) => e.message).join("; "))
    this.name = "GraphQLRequestError"
    this.errors = errors
  }
}

export async function graphqlClient<TResult, TVariables>(
  document: TypedDocumentString<TResult, TVariables>,
  ...[variables]: TVariables extends Record<string, never> ? [] : [TVariables]
): Promise<TResult> {
  const res = await apiFetch("/graphql", {
    method: "POST",
    body: JSON.stringify({
      // TypedDocumentString extends String, so `document` alone would also
      // serialize to the query text — JSON.stringify unwraps a String subclass
      // to its primitive and drops its own properties. Explicit anyway: the
      // wire contract is a string, and nothing here should depend on a reader
      // knowing that particular corner of JSON.stringify.
      query: document.toString(),
      variables,
    }),
  })

  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(`GraphQL request failed (${res.status}): ${text}`)
  }

  const json = await res.json()

  if (json.errors && !json.data) {
    throw new GraphQLRequestError(json.errors)
  }

  // Partial errors (data + errors): some fields are null because a resolver
  // or directive failed. We classify these:
  //
  //   - FORBIDDEN / UNAUTHENTICATED → hard failure. Throw so React Query sees
  //     it as an error, error boundaries fire, and toasts trigger. These are
  //     from the @hasPermission directive and should never be silently
  //     swallowed: a user seeing a partially-blank page with no feedback is
  //     a worse UX than a clear "forbidden" error.
  //
  //   - Anything else → log and return partial data. Resolver-level errors
  //     on optional fields can legitimately leave nulls in the response.
  //
  // If you have a query that genuinely expects some fields to be null for
  // lower-privilege users (e.g. admin-only fields on a shared query), split
  // it into separate queries or gate the field with @include(if: $isAdmin)
  // instead of relying on partial-data fallthrough.
  if (json.errors && json.data) {
    const isHardFailure = json.errors.some((e: GraphQLError) => {
      const code = e.extensions?.code
      return code === "FORBIDDEN" || code === "UNAUTHENTICATED"
    })
    if (isHardFailure) {
      throw new GraphQLRequestError(json.errors)
    }
    console.warn("[GraphQL] Partial errors:", json.errors)
  }

  return json.data as TResult
}

import { GraphQLRequestError } from "@/lib/graphql-client"

/** The first GraphQL `extensions.code` on an error, if it carries one. */
export function graphqlErrorCode(err: unknown): string | null {
  if (!(err instanceof GraphQLRequestError)) return null
  for (const e of err.errors) {
    const code = e.extensions?.code
    if (typeof code === "string") return code
  }
  return null
}

export function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback
}

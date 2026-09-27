import { describe, expect, it, vi } from "vitest"

// apiFetch owns auth, CSRF and connectivity; none of that is under test here.
// Capture what the client hands it and answer with a canned response.
const calls: { path: string; body: unknown }[] = []
const reply: { ok: boolean; status?: number; json: unknown } = {
  ok: true,
  json: { data: { ok: true } },
}

vi.mock("@/services/api-client", () => ({
  apiFetch: (path: string, init: { body: string }) => {
    calls.push({ path, body: JSON.parse(init.body) })
    return Promise.resolve({
      ok: reply.ok,
      status: reply.status ?? 200,
      statusText: "",
      text: () => Promise.resolve(""),
      json: () => Promise.resolve(reply.json),
    })
  },
}))

const { graphqlClient } = await import("@/lib/graphql-client")
const { TypedDocumentString } = await import("@/graphql/gql/graphql")

// Shaped the way codegen writes documents under `documentMode: "string"`.
function doc(query: string) {
  return new TypedDocumentString(query) as unknown as Parameters<
    typeof graphqlClient
  >[0]
}

describe("graphqlClient request body", () => {
  it("sends the document as a query string, not a wrapped object", async () => {
    calls.length = 0
    const query = "\n    query Ping {\n  ping\n}\n    "
    await graphqlClient(doc(query), undefined as never)

    expect(calls).toHaveLength(1)
    // What this pins is the wire contract after the move to string documents:
    // `query` is the document's text, verbatim, and a plain JSON string.
    //
    // Note it does not distinguish `document` from `document.toString()` —
    // JSON.stringify unwraps a String subclass to its primitive either way, so
    // both produce identical bytes. It does catch the shapes that would break
    // the server: a document sent under the wrong key, wrapped in an object, or
    // reduced to "[object Object]".
    expect(calls[0].body).toEqual({ query, variables: undefined })
    expect(typeof (calls[0].body as { query: unknown }).query).toBe("string")
  })

  it("passes variables through untouched", async () => {
    calls.length = 0
    await graphqlClient(doc("query D($id: ID!) { d(id: $id) { id } }"), {
      id: "abc",
    } as never)

    expect((calls[0].body as { variables: unknown }).variables).toEqual({
      id: "abc",
    })
  })

  it("carries inlined fragment definitions to the server", async () => {
    calls.length = 0
    // Every generated document repeats the fragments it spreads; a document
    // that lost them would be rejected with "Unknown fragment".
    const withFragment =
      "\n    query D { d { ...F } }\n    fragment F on D { id }\n    "
    await graphqlClient(doc(withFragment), undefined as never)

    const sent = (calls[0].body as { query: string }).query
    expect(sent).toContain("...F")
    expect(sent).toContain("fragment F on D")
  })
})

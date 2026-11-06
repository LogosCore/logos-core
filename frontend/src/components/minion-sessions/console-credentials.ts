import { graphqlClient } from "@/lib/graphql-client"
import { CredentialsDocument, type CredentialType } from "@/graphql/gql/graphql"
import type { FieldDef } from "@/components/modules/management/field-values"

// A credential param typed in the console: by its name, among the
// operation's credentials of the types the param takes (an id goes as it
// is). A list's names are comma-separated.

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export interface CredentialCandidate {
  id: string
  name: string
}

/** The one credential a name means: exact, else the only one of any case. */
export function matchCredential(
  name: string,
  candidates: readonly CredentialCandidate[],
): { ok: true; id: string } | { ok: false; error: string } {
  const exact = candidates.filter((c) => c.name === name)
  const loose = exact.length > 0 ? exact : candidates.filter((c) => c.name.toLowerCase() === name.toLowerCase())
  if (loose.length === 1) return { ok: true, id: loose[0].id }
  if (loose.length === 0) return { ok: false, error: `No credential named "${name}"` }
  return { ok: false, error: `${loose.length} credentials are named "${name}": pick one in the form` }
}

/** Names typed for a credential param, as the console reads them. */
export function credentialNames(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter((s) => s.trim() !== "")
  if (typeof value !== "string") return []
  return value
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "")
}

/** The args with each credential param's names turned into ids. */
export async function resolveCredentialArgs(
  fields: readonly FieldDef[],
  args: Record<string, unknown>,
  operationId: string,
): Promise<{ ok: true; args: Record<string, unknown> } | { ok: false; error: string }> {
  const out = { ...args }
  for (const f of fields) {
    if ((f.type !== "credential" && f.type !== "credential_list") || !(f.key in args)) continue
    const ids: string[] = []
    for (const name of credentialNames(args[f.key])) {
      if (UUID.test(name)) {
        ids.push(name)
        continue
      }
      const types = (f.accept ?? []) as CredentialType[]
      const page = await graphqlClient(CredentialsDocument, {
        operationId,
        search: name,
        searchFields: ["NAME"],
        types: types.length > 0 ? types : null,
        first: 50,
      })
      const m = matchCredential(
        name,
        page.credentials.edges.map((e) => ({ id: e.node.id, name: e.node.name })),
      )
      if (!m.ok) return { ok: false, error: `${f.key}: ${m.error}${types.length > 0 ? ` (${types.join(", ")})` : ""}` }
      ids.push(m.id)
    }
    out[f.key] = f.type === "credential_list" ? ids : ids[0]
  }
  return { ok: true, args: out }
}

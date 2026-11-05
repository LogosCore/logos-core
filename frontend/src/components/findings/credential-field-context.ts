import { createContext } from "react"
import type { CredentialType } from "@/graphql/gql/graphql"
import type { FieldDef } from "@/components/modules/management/field-values"

/**
 * The operation a credential field picks from: the scoped one, unless a
 * form for another operation's record says otherwise.
 */
export const CredentialOperationContext = createContext<string | null>(null)

/** The credential types a field takes, as the schema's accept names them. */
export function acceptedCredentialTypes(field: FieldDef): CredentialType[] {
  return (field.accept ?? []) as CredentialType[]
}

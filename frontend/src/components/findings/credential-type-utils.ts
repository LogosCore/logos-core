import { useMemo } from "react"
import type {
  CredentialType,
  CredentialTypeSpec,
} from "@/graphql/gql/graphql"
import { useCredentialTypes } from "@/graphql/hooks/credentials"

// The credential types, in the order the UI offers them. Mirrors the GraphQL
// enum; which fields each takes comes from the server (useCredentialTypeSpecs).
export const CREDENTIAL_TYPES: CredentialType[] = [
  "PLAINTEXT",
  "KEY",
  "CERTIFICATE",
  "TICKET",
  "HASH",
  "HEX",
  "OTHER",
]

const TYPE_LABELS: Record<CredentialType, string> = {
  PLAINTEXT: "Plaintext",
  KEY: "Key",
  CERTIFICATE: "Certificate",
  TICKET: "Ticket",
  HASH: "Hash",
  HEX: "Hex",
  OTHER: "Other",
}

export function credentialTypeLabel(t: CredentialType): string {
  return TYPE_LABELS[t] ?? t
}

// What the types were called before they had fields of their own: an OTHER
// the migration made keeps its old one (legacyType) as a hint for converting.
const LEGACY_TYPE_LABELS: Record<string, string> = {
  PASSWORD: "Password",
  SSH_KEY: "SSH key",
  API_KEY: "API key",
  TOKEN: "Token",
  HASH: "Hash",
}

export function legacyCredentialTypeLabel(t: string): string {
  return LEGACY_TYPE_LABELS[t] ?? t
}

// The type the old one most likely is now, to offer first when converting.
const LEGACY_SUCCESSOR: Record<string, CredentialType> = {
  PASSWORD: "PLAINTEXT",
  SSH_KEY: "KEY",
  HASH: "HASH",
}

export function legacyCredentialTypeSuccessor(
  t: string | null | undefined,
): CredentialType | null {
  return (t && LEGACY_SUCCESSOR[t]) || null
}

export type CredentialTypeSpecs = ReadonlyMap<CredentialType, CredentialTypeSpec>

/**
 * The server's type table by type: which fields each takes, in order, and
 * which it requires. Empty until it has loaded (once per session).
 */
export function useCredentialTypeSpecs(): CredentialTypeSpecs {
  const { data } = useCredentialTypes()
  return useMemo(
    () => new Map((data ?? []).map((s) => [s.type, s] as const)),
    [data],
  )
}

/**
 * Parses a comma-separated tag string into a deduplicated lowercase array.
 * Mirrors the backend normalization so the UI displays cleanly after submit.
 */
export function parseTagsText(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of text.split(",")) {
    const t = raw.trim().toLowerCase()
    if (!t || seen.has(t)) continue
    seen.add(t)
    out.push(t)
  }
  return out
}

// Record cards on the canvas: the half that talks to the rest of the app.
// Loading the record a card stands for, opening it the way its chip in a
// page opens it, and picking one for a new card with the pickers the editor's
// slash commands use.
//
// Records are fetched through the query cache under the same keys as the
// chips' own queries (useHost, useCredentialChip, ...), so a drawing and a
// page that show the same host share one request and one cached copy.

import type { QueryClient } from "@tanstack/react-query"
import {
  CredentialChipDocument,
  HashChipDocument,
  HostDocument,
  TaskDocument,
  WikiDocumentLiteDocument,
  type HostFieldsFragment,
} from "@/graphql/gql/graphql"
import { GraphQLRequestError, graphqlClient } from "@/lib/graphql-client"
import { hostKeys } from "@/graphql/hooks/hosts"
import { credentialKeys } from "@/graphql/hooks/credentials"
import { hashKeys } from "@/graphql/hooks/hashes"
import { taskKeys } from "@/graphql/hooks/tasks"
import { wikiKeys } from "@/graphql/hooks/wiki"
import { useHostStore } from "@/stores/hosts"
import { useCredentialStore } from "@/stores/credentials"
import { useHashStore } from "@/stores/hashes"
import { useTaskStore } from "@/stores/tasks"
import { useScopedOperationStore } from "@/stores/scoped-operation"
import { useWikiTreeModeStore } from "@/stores/wiki-tree-mode"
import { toast } from "sonner"
import { truncateHashValue } from "@/components/findings/hash-status-utils"
import { openHostPicker } from "@/components/wiki/wiki-host-picker"
import { openCredentialPicker } from "@/components/wiki/wiki-credential-picker"
import { openHashPicker } from "@/components/wiki/wiki-hash-picker"
import { openWikiDocumentPicker } from "@/components/wiki/wiki-palette-store"
import { openTaskPicker } from "@/components/tasks/task-picker-store"
import type { CardRef, RefKind } from "./drawing-catalogue"
import { humanize } from "./drawing-record-cards"

/** What a card shows for its record. */
export interface RecordWords {
  title: string
  detail: string
  /** An icon over the kind's own: a drawing page draws a pen, not a page. */
  icon?: string
}

export type RecordLookup =
  | ({ status: "ok"; open: (opts: OpenOptions) => void } & RecordWords)
  /** Deleted, or never existed. */
  | { status: "missing" }
  /** Exists, in an operation this viewer cannot see. */
  | { status: "forbidden" }
  /** Could not be asked (offline, server error); says nothing about the
   * record, so nothing is drawn for it. */
  | { status: "error" }

export interface OpenOptions {
  navigate: (to: string) => void
  /** Open a page in a new tab rather than here (Ctrl/⌘ or middle click). */
  newTab?: boolean
}

export const REF_LABELS: Record<RefKind, string> = {
  host: "host",
  credential: "credential",
  hash: "hash",
  task: "task",
  document: "page",
}

// Long enough that panning around a map does not refetch, short enough that
// a reopened drawing sees a rename.
const STALE_MS = 30_000

/** Load the record a card stands for. Never throws. */
export async function lookupRecord(queryClient: QueryClient, ref: CardRef): Promise<RecordLookup> {
  try {
    return await load(queryClient, ref)
  } catch (error) {
    if (error instanceof GraphQLRequestError) {
      if (error.errors.some((e) => e.extensions?.code === "FORBIDDEN")) return { status: "forbidden" }
      return { status: "missing" }
    }
    return { status: "error" }
  }
}

async function load(qc: QueryClient, ref: CardRef): Promise<RecordLookup> {
  const { id } = ref
  switch (ref.kind) {
    case "host": {
      const { host } = await qc.fetchQuery({
        queryKey: hostKeys.detail(id),
        queryFn: () => graphqlClient(HostDocument, { id }),
        staleTime: STALE_MS,
      })
      if (!host) return { status: "missing" }
      return { status: "ok", ...hostWords(host), open: () => openHost(host) }
    }
    case "credential": {
      const { credential: c } = await qc.fetchQuery({
        queryKey: credentialKeys.chip(id),
        queryFn: () => graphqlClient(CredentialChipDocument, { id }),
        staleTime: STALE_MS,
      })
      if (!c) return { status: "missing" }
      const title = c.name || c.username || "Credential"
      const detail = [c.username && c.username !== title ? c.username : "", humanize(c.type)].filter(Boolean).join(" · ")
      return {
        status: "ok",
        title,
        detail,
        open: () => useCredentialStore.getState().openDetailsPanel({ id: c.id, name: c.name }),
      }
    }
    case "hash": {
      const { hash } = await qc.fetchQuery({
        queryKey: hashKeys.chip(id),
        queryFn: () => graphqlClient(HashChipDocument, { id }),
        staleTime: STALE_MS,
      })
      if (!hash) return { status: "missing" }
      const label = truncateHashValue(hash.value)
      return {
        status: "ok",
        title: label,
        detail: humanize(hash.status),
        open: () => useHashStore.getState().openDetailsPanel({ id: hash.id, label }),
      }
    }
    case "task": {
      const { task } = await qc.fetchQuery({
        queryKey: taskKeys.detail(id),
        queryFn: () => graphqlClient(TaskDocument, { id }),
        staleTime: STALE_MS,
      })
      if (!task || task.deletedAt) return { status: "missing" }
      const detail = [humanize(task.stage), task.status !== "UNDEFINED" ? humanize(task.status) : ""]
        .filter(Boolean)
        .join(" · ")
      return {
        status: "ok",
        title: task.name || "Untitled task",
        detail,
        open: () => useTaskStore.getState().openEditDialog({ id: task.id, name: task.name }),
      }
    }
    case "document": {
      const { wikiDocument: doc } = await qc.fetchQuery({
        queryKey: wikiKeys.lite(id),
        queryFn: () => graphqlClient(WikiDocumentLiteDocument, { id }),
        staleTime: STALE_MS,
      })
      if (!doc || doc.deletedAt) return { status: "missing" }
      const drawing = doc.kind === "DRAWING"
      return {
        status: "ok",
        title: doc.title || "Untitled",
        detail: drawing ? "Drawing" : doc.isTemplate ? "Template" : "Wiki page",
        ...(drawing ? { icon: "pen-tool" } : {}),
        open: ({ navigate, newTab }) => {
          const href = `/wiki/${doc.id}`
          if (newTab) window.open(href, "_blank", "noopener,noreferrer")
          else navigate(href)
        },
      }
    }
  }
}

/**
 * The host dialog is an edit form for the operation's hosts, and the wiki
 * mounts it only while it shows an operation's own tree (pages/wiki.tsx), as
 * a host chip is only clickable there. Elsewhere — the Public tree, or no
 * operation scoped — say so rather than do nothing.
 */
function openHost(host: HostFieldsFragment) {
  const scoped = useScopedOperationStore.getState().scopedOperation?.id
  const publicTree = useWikiTreeModeStore.getState().mode === "public"
  if (!scoped || publicTree) {
    toast.info("Hosts open inside their operation", {
      description: "Switch to this page's operation to see the host's details.",
    })
    return
  }
  useHostStore.getState().openEditDialog(host)
}

/** A host's card: its name, and where to find it and what it runs. */
export function hostWords(host: Pick<HostFieldsFragment, "hostname" | "os" | "interfaces">): RecordWords {
  const address = host.interfaces
    .flatMap((i) => i.addresses)
    .map((a) => a.replace(/\/\d+$/, ""))
    .find((a) => a && !a.startsWith("127.") && a !== "::1")
  return {
    title: host.hostname || "Unnamed host",
    detail: [address, host.os].filter(Boolean).join(" · "),
  }
}

/**
 * Ask the operator for a record of `kind` in the operation, with the picker
 * its slash command uses, and hand the picked record to `onPicked`. A picker
 * that is dismissed calls nothing. Credentials can also be created from the
 * picker.
 */
export function pickRecord(
  kind: RefKind,
  operationId: string,
  onPicked: (ref: CardRef) => void,
  excludeDocumentIds: string[] = [],
): void {
  const onPick = (id: string) => onPicked({ kind, id })
  switch (kind) {
    case "host":
      openHostPicker({ operationId, onPick })
      return
    case "credential":
      openCredentialPicker({ operationId, onPick })
      return
    case "hash":
      openHashPicker({ operationId, onPick })
      return
    case "task":
      openTaskPicker({
        operationId,
        title: "Add task to drawing",
        description: "Pick a task in this operation to draw as a card.",
        onPick: (task) => onPick(task.id),
      })
      return
    case "document":
      openWikiDocumentPicker({
        operationId,
        excludeIds: excludeDocumentIds,
        title: "Add page to drawing",
        description: "Pick a wiki page in this operation to draw as a card.",
        onPick: (doc) => onPick(doc.id),
      })
      return
  }
}

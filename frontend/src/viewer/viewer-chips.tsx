// Static versions of the editor's four reference chips.
//
// The editor's chips carry only an id and look everything else up live.
// The exporter writes what they would have found onto the node, under the
// `export` attribute (htmlbundle.ExportAttr), and these render it with the
// editor chips' own markup and classes, so wiki-chips.css styles them the
// same way.

import { useState } from "react"
import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react"
import {
  CheckCircle2Icon,
  CircleDashedIcon,
  FileTextIcon,
  HashIcon,
  KeyIcon,
  LockIcon,
  LockOpenIcon,
  ServerIcon,
  XCircleIcon,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { useSha1Hashes } from "@/lib/sha1"
import { DocumentIcon } from "@/components/wiki/document-icon"
import { HostIcon } from "@/components/findings/host-icon"
import { hashStatusLabel, truncateHashValue } from "@/components/findings/hash-status-utils"
import type { HashStatus } from "@/graphql/gql/graphql"
import "@/components/wiki/wiki-chips.css"
import { RecordDialogFor } from "./viewer-record-views"

type Exported = Record<string, unknown> | null

function exported(props: NodeViewProps): Exported {
  const value = props.node.attrs.export as unknown
  return value && typeof value === "object" ? (value as Record<string, unknown>) : null
}

const str = (v: unknown) => (typeof v === "string" ? v : "")

function Missing({ className, icon, label, title }: { className: string; icon: React.ReactNode; label: string; title: string }) {
  return (
    <NodeViewWrapper as="span" className={cn(className, `${className.split(" ")[0]}--missing`)} title={title}>
      {icon}
      <span className={`${className.split(" ")[0]}__name`}>{label}</span>
    </NodeViewWrapper>
  )
}

export function StaticDocumentChip(props: NodeViewProps) {
  const e = exported(props)
  if (!e || e.missing) {
    return (
      <Missing
        className="wiki-document-chip"
        icon={<FileTextIcon className="size-3.5" />}
        label="Unavailable page"
        title="This page was not part of the export"
      />
    )
  }
  const title = str(e.title) || "Untitled"
  const href = str(e.href)
  const body = (
    <>
      <span className="wiki-document-chip__icon">
        <DocumentIcon
          emoji={str(e.emoji)}
          icon={str(e.icon)}
          color={str(e.color)}
          isDrawing={e.kind === "drawing"}
        />
      </span>
      <span className="wiki-document-chip__name">{title}</span>
    </>
  )
  return (
    <NodeViewWrapper as="span">
      {href ? (
        <a href={href} className="wiki-document-chip">
          {body}
        </a>
      ) : (
        <span className="wiki-document-chip" title="This page is not in the export">
          {body}
        </span>
      )}
    </NodeViewWrapper>
  )
}

function useOpen() {
  const [open, setOpen] = useState(false)
  const onClick = (ev: React.MouseEvent) => {
    ev.preventDefault()
    setOpen(true)
  }
  return { open, setOpen, onClick }
}

export function StaticHostChip(props: NodeViewProps) {
  const e = exported(props)
  const dialog = useOpen()
  if (!e || e.missing) {
    return (
      <Missing
        className="wiki-host-chip"
        icon={<ServerIcon className="size-3.5" />}
        label="Host unavailable"
        title="This host was not part of the export"
      />
    )
  }
  const label = str(e.hostname) || "Unnamed host"
  const icon = (
    <HostIcon emoji={str(e.emoji)} icon={str(e.icon)} color={str(e.color)} os={str(e.os)} size={14} className="wiki-host-chip__icon" />
  )
  return (
    <NodeViewWrapper as="span">
      <button
        type="button"
        onClick={dialog.onClick}
        className="wiki-host-chip"
        title={[label, str(e.description), str(e.os)].filter(Boolean).join("\n")}
      >
        {icon}
        <span className="wiki-host-chip__name">{label}</span>
      </button>
      <RecordDialogFor kind="host" data={e} open={dialog.open} onOpenChange={dialog.setOpen} />
    </NodeViewWrapper>
  )
}

export function StaticHashChip(props: NodeViewProps) {
  const e = exported(props)
  const dialog = useOpen()
  if (!e || e.missing) {
    return (
      <Missing
        className="wiki-hash-chip"
        icon={<HashIcon className="size-3.5" />}
        label="Hash unavailable"
        title="This hash was not part of the export"
      />
    )
  }
  const value = str(e.value)
  const status = (str(e.status) || "NOT_PROCESSED") as HashStatus
  const cracked = status === "CRACKED"
  return (
    <NodeViewWrapper as="span">
      <button
        type="button"
        onClick={dialog.onClick}
        className={cn("wiki-hash-chip", cracked && "wiki-hash-chip--cracked")}
        title={`${value}\n${hashStatusLabel(status)}`}
      >
        <HashIcon className="size-3.5 wiki-hash-chip__icon" />
        <span className="wiki-hash-chip__name">{truncateHashValue(value)}</span>
        {cracked ? (
          <LockOpenIcon className="wiki-hash-chip__status wiki-hash-chip__status--cracked size-3" />
        ) : (
          <LockIcon className="wiki-hash-chip__status wiki-hash-chip__status--uncracked size-3" />
        )}
      </button>
      <RecordDialogFor kind="hash" data={e} open={dialog.open} onOpenChange={dialog.setOpen} />
    </NodeViewWrapper>
  )
}

const VALIDITY_ICON = { UNKNOWN: CircleDashedIcon, VALID: CheckCircle2Icon, INVALID: XCircleIcon }
const VALIDITY_CLASS = {
  UNKNOWN: "wiki-credential-chip__validity--unknown",
  VALID: "wiki-credential-chip__validity--valid",
  INVALID: "wiki-credential-chip__validity--invalid",
}

interface CredentialKey {
  name: string
  content: string
}

/** Mirrors the editor chip: a key shows as its name and the start of its
 * SHA-1, never its content. The content is in the dialog. */
const KEY_HASH_DISPLAY_LEN = 12

export function StaticCredentialChip(props: NodeViewProps) {
  const e = exported(props)
  const keys = (Array.isArray(e?.keys) ? e.keys : []) as CredentialKey[]
  const hashes = useSha1Hashes(keys.map((k) => k.content))
  const dialog = useOpen()
  if (!e || e.missing) {
    return (
      <Missing
        className="wiki-credential-chip"
        icon={<KeyIcon className="size-3.5" />}
        label="Credential unavailable"
        title="This credential was not part of the export"
      />
    )
  }
  const name = str(e.name) || "Credential"
  const username = str(e.username)
  const password = str(e.password)
  const creds = username && password ? `${username}:${password}` : username || password
  const validity = (str(e.validity) || "UNKNOWN") as keyof typeof VALIDITY_ICON
  const ValidityIcon = VALIDITY_ICON[validity] ?? VALIDITY_ICON.UNKNOWN
  const title = [name, creds, ...keys.map((k, i) => `${k.name}: ${hashes[i] ?? "hashing…"}`), "Click for details"]
    .filter(Boolean)
    .join("\n")
  return (
    <NodeViewWrapper as="span">
      <button
        type="button"
        onClick={dialog.onClick}
        className={cn("wiki-credential-chip", validity === "INVALID" && "wiki-credential-chip--invalid")}
        title={title}
      >
        <KeyIcon className="size-3.5 wiki-credential-chip__icon" />
        <span className="wiki-credential-chip__name">{name}</span>
        {creds && <span className="wiki-credential-chip__segment">({creds})</span>}
        {keys.map((k, i) => (
          <span key={`${k.name}-${i}`} className="wiki-credential-chip__segment wiki-credential-chip__segment--key">
            ({k.name}:{hashes[i]?.slice(0, KEY_HASH_DISPLAY_LEN) ?? "…"})
          </span>
        ))}
        <ValidityIcon className={cn("wiki-credential-chip__validity size-3", VALIDITY_CLASS[validity])} />
      </button>
      <RecordDialogFor kind="credential" data={e} open={dialog.open} onOpenChange={dialog.setOpen} />
    </NodeViewWrapper>
  )
}

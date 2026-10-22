// The details dialog for one exported record, by kind: what a host, hash or
// credential chip opens, and what a record card in a drawing opens. The data
// is what the exporter wrote for it (htmlbundle's chip transforms and
// drawingRefs), so both paths show the same thing for the same record.

import { HashIcon, KeyIcon, ListTodoIcon, ServerIcon } from "lucide-react"
import { hashStatusLabel } from "@/components/findings/hash-status-utils"
import type { HashStatus } from "@/graphql/gql/graphql"
import { RecordDialog, type RecordRow } from "./viewer-record-dialog"

export type ExportedRecord = Record<string, unknown>

const str = (v: unknown) => (typeof v === "string" ? v : "")
const num = (v: unknown) => (typeof v === "number" ? String(v) : "")

const TYPE_LABEL: Record<string, string> = {
  PASSWORD: "Password",
  SSH_KEY: "SSH key",
  API_KEY: "API key",
  TOKEN: "Token",
  HASH: "Hash",
  OTHER: "Other",
}

const VALIDITY_LABEL: Record<string, string> = {
  UNKNOWN: "Not tested",
  VALID: "Works",
  INVALID: "Does not work",
}

const STAGE_LABEL: Record<string, string> = {
  BACKLOG: "Backlog",
  TODO: "To do",
  IN_PROCESS: "In process",
  DONE: "Done",
}

const STATUS_LABEL: Record<string, string> = { SUCCESS: "Succeeded", FAIL: "Failed" }

export type RecordKind = "host" | "hash" | "credential" | "task"

interface RecordDialogForProps {
  kind: RecordKind
  data: ExportedRecord
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function RecordDialogFor({ kind, data: e, open, onOpenChange }: RecordDialogForProps) {
  const common = { open, onOpenChange }
  switch (kind) {
    case "host": {
      const label = str(e.hostname) || "Unnamed host"
      const interfaces = (Array.isArray(e.interfaces) ? e.interfaces : []) as { name?: string; addresses?: string[] }[]
      return (
        <RecordDialog
          {...common}
          icon={<ServerIcon className="size-4" />}
          title={label}
          description={str(e.description) || undefined}
          rows={[
            { label: "Hostname", value: label, mono: true },
            { label: "Operating system", value: str(e.os) },
            ...interfaces
              .filter((i) => i.addresses?.length)
              .map((i) => ({
                label: i.name ? `Interface ${i.name}` : "Interface",
                value: i.addresses!.join(", "),
                mono: true,
              })),
          ]}
        />
      )
    }
    case "hash": {
      const value = str(e.value)
      const status = (str(e.status) || "NOT_PROCESSED") as HashStatus
      return (
        <RecordDialog
          {...common}
          icon={<HashIcon className="size-4" />}
          title="Hash"
          description={hashStatusLabel(status)}
          rows={[{ label: "Value", value, mono: true, block: value.length > 64 }]}
        />
      )
    }
    case "credential": {
      const keys = (Array.isArray(e.keys) ? e.keys : []) as { name: string; content: string }[]
      const properties = (Array.isArray(e.properties) ? e.properties : []) as { name: string; value: string }[]
      const tags = (Array.isArray(e.tags) ? e.tags : []) as string[]
      const validity = str(e.validity) || "UNKNOWN"
      return (
        <RecordDialog
          {...common}
          icon={<KeyIcon className="size-4" />}
          title={str(e.name) || "Credential"}
          description={[TYPE_LABEL[str(e.type)] ?? str(e.type), VALIDITY_LABEL[validity]].filter(Boolean).join(" · ")}
          rows={[
            { label: "Username", value: str(e.username), mono: true },
            { label: "Password", value: str(e.password), mono: true },
            ...keys.map((k) => ({ label: k.name || "Key", value: k.content, mono: true, block: true })),
            ...properties.map((p) => ({ label: p.name, value: p.value, mono: true })),
          ]}
          tags={tags}
        />
      )
    }
    case "task": {
      const rows: RecordRow[] = [
        { label: "Description", value: str(e.description), block: str(e.description).includes("\n") },
        { label: "Summary", value: str(e.summary) },
        { label: "Risk", value: [num(e.riskScore) && `${num(e.riskScore)}/10`, str(e.riskDescription)].filter(Boolean).join(" · ") },
        {
          label: "Profit",
          value: [num(e.profitScore) && `${num(e.profitScore)}/10`, str(e.profitDescription)].filter(Boolean).join(" · "),
        },
      ]
      return (
        <RecordDialog
          {...common}
          icon={<ListTodoIcon className="size-4" />}
          title={str(e.name) || "Task"}
          description={[STAGE_LABEL[str(e.stage)] ?? str(e.stage), STATUS_LABEL[str(e.status)]].filter(Boolean).join(" · ")}
          rows={rows}
        />
      )
    }
  }
}

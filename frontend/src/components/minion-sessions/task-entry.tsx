import { useState } from "react"
import {
  BanIcon,
  CheckIcon,
  CircleDashedIcon,
  CopyIcon,
  FileIcon,
  FolderIcon,
  LoaderIcon,
  RotateCcwIcon,
  SendIcon,
  SquareIcon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useCancelMinionTask, useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { lineOf, type CommandDef } from "./command-line"
import { formatInterval } from "./sessions-layout"

// One task in a session's console: who gave it and when, the command as
// typed, where it stands, and what came back (output as text, an ls as a
// table, any other data as JSON).

type Task = MinionTaskFieldsFragment

const FINISHED = new Set(["DONE", "FAILED", "CANCELLED"])

/** HH:MM:SS of a time. */
function clock(at: string): string {
  return new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
}

/** How long it ran, from when it went out to its end. */
function took(t: Task): string | null {
  const from = t.startedAt ?? t.sentAt
  if (!from || !t.doneAt) return null
  return formatInterval(Math.max(0, Date.parse(t.doneAt) - Date.parse(from)))
}

function State({ t }: { t: Task }) {
  const stopping = t.cancelRequestedAt && !FINISHED.has(t.state)
  const muted = "text-muted-foreground"
  let content: React.ReactNode
  switch (t.state) {
    case "QUEUED":
      content = (
        <span className={muted} title="It goes out when the minion next checks in and asks for tasks">
          <CircleDashedIcon /> waits for the next check-in
        </span>
      )
      break
    case "SENT":
      content = (
        <span className={muted} title={t.sentAt ? `Sent ${formatAbsolute(t.sentAt)}` : undefined}>
          <SendIcon /> sent{t.sendCount > 1 ? ` ×${t.sendCount}` : ""}, not acked yet
        </span>
      )
      break
    case "RUNNING":
      content = (
        <span className="text-sky-600 dark:text-sky-400">
          <LoaderIcon className="animate-spin" /> running
        </span>
      )
      break
    case "DONE":
      content = (
        <span className="text-emerald-600 dark:text-emerald-400">
          <CheckIcon /> {t.exit != null ? `exit ${t.exit}` : "done"}
          {took(t) && <span className={muted}> · {took(t)}</span>}
        </span>
      )
      break
    case "FAILED":
      content = (
        <span className="text-red-600 dark:text-red-400">
          <XIcon /> failed{took(t) && <span className={muted}> · {took(t)}</span>}
        </span>
      )
      break
    case "CANCELLED":
      content = (
        <span className={muted}>
          <BanIcon /> cancelled
        </span>
      )
      break
    default:
      content = <span className={muted}>{t.state.toLowerCase()}</span>
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs [&_svg]:size-3.5 [&_svg]:shrink-0 [&>span]:inline-flex [&>span]:items-center [&>span]:gap-1">
      {content}
      {stopping && <span className="text-amber-600 dark:text-amber-400">· stopping</span>}
    </span>
  )
}

export function TaskEntry({ task: t, command, canWrite }: { task: Task; command?: CommandDef; canWrite: boolean }) {
  const cancel = useCancelMinionTask()
  const queue = useQueueMinionTask()
  const finished = FINISHED.has(t.state)
  const line = lineOf(t.command, t.args as Record<string, unknown> | null, command)
  return (
    <div className="group/task border-b px-4 py-2 last:border-b-0">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="font-mono text-xs text-muted-foreground tabular-nums" title={formatAbsolute(t.queuedAt)}>
          {clock(t.queuedAt)}
        </span>
        <span className="text-xs text-muted-foreground">{t.issuedByName ?? "someone gone"}</span>
        <code className="min-w-0 flex-1 truncate font-mono text-sm" title={line}>
          {line}
        </code>
        <State t={t} />
        <span className="flex items-center gap-1 opacity-0 transition-opacity group-hover/task:opacity-100 focus-within:opacity-100">
          {t.output && (
            <Button variant="ghost" size="icon-xs" title="Copy the output" aria-label="Copy the output"
              onClick={() => void copyToClipboard(t.output, "output")}>
              <CopyIcon />
            </Button>
          )}
          {canWrite && !finished && !t.cancelRequestedAt && (
            <Button variant="ghost" size="icon-xs" title="Stop it" aria-label="Stop the task" disabled={cancel.isPending}
              onClick={() => cancel.mutate(t.id, { onError: (err) => toast.error(errorMessage(err, "Could not stop the task")) })}>
              <SquareIcon />
            </Button>
          )}
          {canWrite && finished && (
            <Button variant="ghost" size="icon-xs" title="Run it again" aria-label="Run the task again" disabled={queue.isPending}
              onClick={() =>
                queue.mutate(
                  { sessionId: t.sessionId, command: t.command, args: (t.args as Record<string, unknown> | null) ?? null },
                  { onError: (err) => toast.error(errorMessage(err, "Could not queue the task")) },
                )
              }>
              <RotateCcwIcon />
            </Button>
          )}
        </span>
      </div>
      {t.err && <div className="mt-1 font-mono text-xs whitespace-pre-wrap text-red-600 dark:text-red-400">{t.err}</div>}
      <Output t={t} />
      <Data t={t} />
    </div>
  )
}

function Output({ t }: { t: Task }) {
  if (!t.output) return null
  return (
    <div className="mt-1">
      <pre className="max-h-96 overflow-auto rounded-md bg-muted/50 px-3 py-2 font-mono text-xs whitespace-pre-wrap break-all">
        {t.output}
      </pre>
      {(!t.outputUtf8 || t.outputTruncated) && (
        <p className="mt-1 text-xs text-muted-foreground">
          {!t.outputUtf8 && "Not all of it is UTF-8: bytes that are not show as �. "}
          {t.outputTruncated && "Cut at 4 MiB: the rest was dropped."}
        </p>
      )}
    </div>
  )
}

interface Entry {
  name: string
  dir?: boolean
  size?: number
  mode?: string
  modified?: string
}

function isEntries(v: unknown): v is Entry[] {
  return Array.isArray(v) && v.every((e) => typeof e === "object" && e !== null && typeof (e as Entry).name === "string")
}

/** A byte count, short: "4.2 KiB". */
function size(n?: number): string {
  if (n === undefined) return ""
  const units = ["B", "KiB", "MiB", "GiB", "TiB"]
  let v = n
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return u === 0 ? `${v} B` : `${v.toFixed(1)} ${units[u]}`
}

function Data({ t }: { t: Task }) {
  const [all, setAll] = useState(false)
  const data = t.data as Record<string, unknown> | null
  if (!data) return null
  const { entries, path, ...rest } = data
  if (isEntries(entries)) {
    // Directories first, then by name.
    const sorted = [...entries].sort((a, b) => Number(!!b.dir) - Number(!!a.dir) || a.name.localeCompare(b.name))
    const shown = all ? sorted : sorted.slice(0, 200)
    return (
      <div className="mt-1 overflow-hidden rounded-md border">
        {typeof path === "string" && (
          <div className="border-b bg-muted/40 px-3 py-1 font-mono text-xs text-muted-foreground">
            {path} · {entries.length} {entries.length === 1 ? "entry" : "entries"}
          </div>
        )}
        <div className="max-h-96 overflow-auto">
          <table className="w-full text-xs">
            <tbody>
              {shown.map((e) => (
                <tr key={e.name} className="border-b last:border-b-0 hover:bg-muted/40">
                  <td className="w-full max-w-0 truncate px-3 py-1 font-mono" title={e.name}>
                    <span className="inline-flex items-center gap-1.5">
                      {e.dir ? <FolderIcon className="size-3.5 text-amber-600 dark:text-amber-400" /> : <FileIcon className="size-3.5 text-muted-foreground" />}
                      {e.name}
                    </span>
                  </td>
                  <td className="px-3 py-1 text-right whitespace-nowrap text-muted-foreground tabular-nums">{e.dir ? "" : size(e.size)}</td>
                  <td className="px-3 py-1 font-mono whitespace-nowrap text-muted-foreground">{e.mode}</td>
                  <td className="px-3 py-1 whitespace-nowrap text-muted-foreground" title={e.modified ? formatAbsolute(e.modified) : undefined}>
                    {e.modified ? new Date(e.modified).toLocaleString() : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!all && sorted.length > shown.length && (
            <button type="button" className="w-full cursor-pointer px-3 py-1 text-xs text-muted-foreground hover:bg-muted/40" onClick={() => setAll(true)}>
              Show all {sorted.length}
            </button>
          )}
        </div>
        {Object.keys(rest).length > 0 && <Json value={rest} />}
      </div>
    )
  }
  return <Json value={data} />
}

function Json({ value }: { value: unknown }) {
  return (
    <pre className={cn("mt-1 max-h-96 overflow-auto rounded-md bg-muted/50 px-3 py-2 font-mono text-xs whitespace-pre-wrap break-all")}>
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}

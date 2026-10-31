import { useMemo, useRef, useState, type KeyboardEvent } from "react"
import { ChevronRightIcon, PlayIcon, SlidersHorizontalIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { errorMessage } from "@/components/modules/management/api-errors"
import { ParamFields } from "@/components/modules/management/param-fields"
import { checkDraft, emptyDraft, type Draft, type FieldDef } from "@/components/modules/management/field-values"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { cn } from "@/lib/utils"
import { commandOf, parseCommandLine, usage, words, type CommandDef } from "./command-line"

// The console's command line: a command of the session's build typed as a
// line (command-line.ts), with its commands offered as the first word is
// typed and how the line reads shown under it; ↑ and ↓ go through what was
// run here. The form beside it fills a command's params field by field.

/** A command as the console uses it: its params as the forms know them too. */
export interface ConsoleCommand extends CommandDef {
  fields: readonly FieldDef[]
}

// What was run, per session, for ↑ and ↓: kept while the page is.
const histories = new Map<string, string[]>()

export function CommandBar({
  sessionId,
  commands,
  disabled,
  disabledReason,
  onQueued,
}: {
  sessionId: string
  commands: readonly ConsoleCommand[]
  disabled: boolean
  /** Why it is disabled, said in its place. */
  disabledReason?: string
  /** A task this queued. */
  onQueued?: (task: MinionTaskFieldsFragment) => void
}) {
  const queue = useQueueMinionTask()
  const [line, setLine] = useState("")
  const [pick, setPick] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // Where ↑ and ↓ are in the history; null: the line being typed.
  const [back, setBack] = useState<number | null>(null)
  const draft = useRef("")
  const input = useRef<HTMLInputElement>(null)
  const history = histories.get(sessionId) ?? []

  // The commands the first word could be, while it is being typed.
  const typingCommand = words(line).length <= 1 && !/\s$/.test(line)
  const prefix = line.trim().toLowerCase()
  const offered = useMemo(
    () => (typingCommand ? commands.filter((c) => c.id.toLowerCase().startsWith(prefix)) : []),
    [typingCommand, prefix, commands],
  )
  const command = commandOf(line, commands)
  const parsed = line.trim() === "" ? null : parseCommandLine(line, commands)

  const run = (text: string) => {
    const p = parseCommandLine(text, commands)
    if (!p.ok) {
      setError(p.error)
      return
    }
    setError(null)
    queue.mutate(
      { sessionId, command: p.command.id, args: Object.keys(p.args).length > 0 ? p.args : null },
      {
        onSuccess: (data) => {
          onQueued?.(data.queueMinionTask)
          histories.set(sessionId, [...history.filter((h) => h !== text), text].slice(-100))
          setLine("")
          setBack(null)
        },
        onError: (err) => setError(errorMessage(err, "Could not queue the task")),
      },
    )
  }

  const complete = (c: CommandDef) => {
    setLine(`${c.id} `)
    setPick(0)
    input.current?.focus()
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (offered.length > 0 && prefix !== "" && !(offered.length === 1 && offered[0].id === prefix)) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        setPick((p) => (p + (e.key === "ArrowDown" ? 1 : offered.length - 1)) % offered.length)
        return
      }
      if (e.key === "Tab" || e.key === "Enter") {
        e.preventDefault()
        complete(offered[Math.min(pick, offered.length - 1)])
        return
      }
    }
    if (e.key === "Enter") {
      e.preventDefault()
      if (line.trim() !== "") run(line)
      return
    }
    if (e.key === "ArrowUp" && history.length > 0) {
      e.preventDefault()
      if (back === null) draft.current = line
      const i = back === null ? history.length - 1 : Math.max(0, back - 1)
      setBack(i)
      setLine(history[i])
      return
    }
    if (e.key === "ArrowDown" && back !== null) {
      e.preventDefault()
      const i = back + 1
      if (i >= history.length) {
        setBack(null)
        setLine(draft.current)
      } else {
        setBack(i)
        setLine(history[i])
      }
    }
  }

  if (disabled) {
    return <div className="border-t px-4 py-2.5 text-sm text-muted-foreground">{disabledReason}</div>
  }

  return (
    <div className="relative border-t">
      {/* The commands the first word could be, above the line. */}
      {offered.length > 0 && prefix !== "" && !(offered.length === 1 && offered[0].id === prefix) && (
        <div className="absolute inset-x-2 bottom-full z-20 mb-1 overflow-hidden rounded-md border bg-popover shadow-md" role="listbox">
          {offered.map((c, i) => (
            <button
              key={c.id}
              type="button"
              role="option"
              aria-selected={i === pick}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => complete(c)}
              className={cn("flex w-full cursor-pointer items-baseline gap-3 px-3 py-1.5 text-left text-sm", i === pick && "bg-muted")}
            >
              <code className="font-mono">{usage(c)}</code>
              <span className="truncate text-xs text-muted-foreground">{c.label}</span>
            </button>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2 px-3 py-2">
        <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground" />
        <input
          ref={input}
          value={line}
          onChange={(e) => {
            setLine(e.target.value)
            setError(null)
            setBack(null)
            setPick(0)
          }}
          onKeyDown={onKeyDown}
          spellCheck={false}
          autoComplete="off"
          aria-label="Command"
          placeholder={commands.length > 0 ? `${commands.map((c) => c.id).join(", ")}` : "No commands"}
          className="min-w-0 flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-muted-foreground/60"
        />
        <ParamsForm sessionId={sessionId} commands={commands} initial={command} onQueued={onQueued} />
        <Button size="xs" disabled={queue.isPending || line.trim() === ""} onClick={() => run(line)}>
          <PlayIcon />
          Run
        </Button>
      </div>
      {/* How the line reads, or why it does not. */}
      {(error || (parsed && command)) && (
        <div className={cn("px-9 pb-2 font-mono text-xs", error || (parsed && !parsed.ok) ? "text-red-600 dark:text-red-400" : "text-muted-foreground")}>
          {error ?? (parsed && !parsed.ok ? parsed.error : parsed?.ok ? readsAs(parsed.command, parsed.args) : null)}
        </div>
      )}
    </div>
  )
}

/** How a parsed line reads: each param's value, defaults marked. */
function readsAs(command: CommandDef, args: Record<string, unknown>): string {
  const parts = command.params.map((p) => {
    if (p.key in args) return `${p.key}=${JSON.stringify(args[p.key])}`
    if (p.default !== null && p.default !== undefined) return `${p.key}=${JSON.stringify(p.default)} (default)`
    return null
  })
  return [command.id, ...parts.filter((p) => p !== null)].join("  ")
}

/** A command's params, field by field: for those with many, or unusual types. */
function ParamsForm({
  sessionId,
  commands,
  initial,
  onQueued,
}: {
  sessionId: string
  commands: readonly ConsoleCommand[]
  initial?: CommandDef
  onQueued?: (task: MinionTaskFieldsFragment) => void
}) {
  const queue = useQueueMinionTask()
  const [open, setOpen] = useState(false)
  const [id, setId] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, Draft>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const command = commands.find((c) => c.id === id)

  const submit = () => {
    if (!command) return
    const args: Record<string, unknown> = {}
    const problems: Record<string, string> = {}
    for (const f of command.fields) {
      const r = checkDraft(f, drafts[f.key] ?? emptyDraft(f, { secretAsText: true }))
      if (!r.ok) problems[f.key] = r.error
      else if (r.value !== null && r.value !== undefined) args[f.key] = r.value
    }
    setErrors(problems)
    if (Object.keys(problems).length > 0) return
    queue.mutate(
      { sessionId, command: command.id, args: Object.keys(args).length > 0 ? args : null },
      {
        onSuccess: (data) => {
          onQueued?.(data.queueMinionTask)
          setOpen(false)
          setDrafts({})
        },
        onError: (err) => toast.error(errorMessage(err, "Could not queue the task")),
      },
    )
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) {
          setId(initial?.id ?? commands[0]?.id ?? null)
          setDrafts({})
          setErrors({})
        }
      }}
    >
      <PopoverTrigger
        render={<Button variant="ghost" size="icon-xs" title="Fill a command's params in a form" aria-label="Command form" disabled={commands.length === 0} />}
      >
        <SlidersHorizontalIcon />
      </PopoverTrigger>
      <PopoverContent align="end" side="top" className="w-96">
        <div className="flex flex-col gap-4">
          <Select value={id} onValueChange={(v) => { setId(typeof v === "string" ? v : null); setDrafts({}); setErrors({}) }}>
            <SelectTrigger className="w-full" aria-label="Command">
              <SelectValue placeholder="Command" />
            </SelectTrigger>
            <SelectContent>
              {commands.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.label} <span className="font-mono text-xs text-muted-foreground">{c.id}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {command?.description && <p className="text-sm text-muted-foreground">{command.description}</p>}
          {command && command.fields.length > 0 && (
            <ParamFields
              params={command.fields}
              drafts={drafts}
              onChange={(key, d) => setDrafts((ds) => ({ ...ds, [key]: d }))}
              errors={errors}
              idPrefix={`task-${sessionId}`}
            />
          )}
          <Button onClick={submit} disabled={!command || queue.isPending}>
            <PlayIcon />
            Run
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

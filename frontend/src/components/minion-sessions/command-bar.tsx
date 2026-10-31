import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import { ChevronRightIcon, PlayIcon, SlidersHorizontalIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { Draft, FieldDef } from "@/components/modules/management/field-values"
import { CommandFormDialog } from "@/components/command-form/command-form-dialog"
import { draftsFromTexts } from "@/components/command-form/command-drafts"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { useQueueMinionTask } from "@/graphql/hooks/minion-tasks"
import { cn } from "@/lib/utils"
import { commandOf, lineOf, lineTexts, parseCommandLine, usage, words, type CommandDef } from "./command-line"

// The console's command line: a command of the session's build typed as a
// line (command-line.ts), with its commands offered as the first word is
// typed and how the line reads shown under it. Enter runs the line;
// Shift+Enter (or the form button) opens it in the command form, its params
// filled from what was typed (an empty line opens it with a command to
// pick). ↑ and ↓ go through what was run here (the session's tasks, as
// lines), and / anywhere else on the page comes to it.

/** A command as the console uses it: its params as the forms know them too. */
export interface ConsoleCommand extends CommandDef {
  fields: readonly FieldDef[]
}

export function CommandBar({
  sessionId,
  commands,
  disabled,
  disabledReason,
  history,
  onQueued,
}: {
  sessionId: string
  commands: readonly ConsoleCommand[]
  disabled: boolean
  /** Why it is disabled, said in its place. */
  disabledReason?: string
  /** The lines ↑ and ↓ go through, oldest first: the session's tasks (commandHistory). */
  history: readonly string[]
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
  // The command form: its command (null: one to pick), where its fields
  // start, and which were typed; key starts it over. Kept while it closes,
  // so it does not change on its way out.
  const [formOpen, setFormOpen] = useState(false)
  const [form, setForm] = useState<{
    id: string | null
    picking: boolean
    drafts?: Record<string, Draft>
    typed: string[]
    key: number
  } | null>(null)
  const formCommand = form?.id ? commands.find((c) => c.id === form.id) : undefined
  const openings = useRef(0)
  const nextKey = () => (openings.current += 1)

  // / comes to the line: the one on screen (the work pane keeps the tabs
  // behind it mounted), and not while something else takes typing or holds
  // the keys (a field, a dialog, a menu).
  useEffect(() => {
    if (disabled) return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key !== "/" || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return
      const el = input.current
      if (!el || el.getClientRects().length === 0 || document.activeElement === el) return
      const target = e.target instanceof HTMLElement ? e.target : null
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return
      if (target?.closest('[role="dialog"], [role="alertdialog"], [role="menu"]')) return
      e.preventDefault()
      el.focus()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [disabled])

  // The commands the first word could be, while it is being typed.
  // Not while going through the history: ↑ and ↓ are its then.
  const typingCommand = back === null && words(line).length <= 1 && !/\s$/.test(line)
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

  // Opens the command form on the line: its command's params filled from
  // what was typed for them; an empty line, to pick a command.
  const openForm = () => {
    if (line.trim() === "") {
      setForm({ id: null, picking: true, typed: [], key: nextKey() })
      setFormOpen(true)
      return
    }
    const lt = lineTexts(line, commands)
    const c = lt.command ? commands.find((x) => x.id === lt.command!.id) : undefined
    if (!c) {
      setError(`No command "${words(line)[0].text}": ${commands.map((x) => x.id).join(", ") || "none"}`)
      return
    }
    setError(null)
    setForm({ id: c.id, picking: false, drafts: draftsFromTexts(c.fields, lt.texts), typed: [...lt.texts.keys()], key: nextKey() })
    setFormOpen(true)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && e.shiftKey) {
      e.preventDefault()
      openForm()
      return
    }
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
    // As tall as the line it stands for, said or not.
    return <div className="min-h-10 border-t px-4 py-2.5 text-sm text-muted-foreground">{disabledReason}</div>
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
          title="The command line (/)"
          className="peer min-w-0 flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-muted-foreground/60"
        />
        <kbd className="rounded border px-1.5 font-mono text-[11px] text-muted-foreground peer-focus:hidden" title="Press / to type a command">
          /
        </kbd>
        <span className="hidden shrink-0 items-center gap-1 text-[11px] text-muted-foreground peer-focus:inline-flex">
          <kbd className="rounded border px-1 font-mono">↵</kbd> run
          <kbd className="ml-1 rounded border px-1 font-mono">⇧↵</kbd> form
        </span>
        <Button variant="ghost" size="icon-xs" title="Fill the command's params in a form (Shift+Enter)" aria-label="Command form"
          disabled={commands.length === 0} onMouseDown={(e) => e.preventDefault()} onClick={openForm}>
          <SlidersHorizontalIcon />
        </Button>
        <Button size="xs" disabled={queue.isPending || line.trim() === ""} onClick={() => run(line)}>
          <PlayIcon />
          Run
        </Button>
      </div>
      <CommandFormDialog
        open={formOpen}
        onOpenChange={(o) => {
          if (o) return
          setFormOpen(false)
          input.current?.focus()
        }}
        formKey={`${form?.key}-${form?.id}`}
        title={formCommand ? formCommand.label : "Run a command"}
        description={
          formCommand ? (
            <>
              <code className="font-mono text-xs">{usage(formCommand)}</code>
              {formCommand.description && <span className="mt-1 block">{formCommand.description}</span>}
            </>
          ) : (
            "Pick one of the session's commands."
          )
        }
        before={
          form?.picking && (
            <Select
              value={form.id}
              onValueChange={(v) => typeof v === "string" && setForm({ ...form, id: v, drafts: undefined, typed: [], key: nextKey() })}
            >
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
          )
        }
        fields={formCommand?.fields ?? []}
        initialDrafts={form?.drafts}
        showErrorsFor={form?.typed}
        after={(values) =>
          formCommand && (
            <p className="truncate font-mono text-xs text-muted-foreground" title={values ? lineOf(formCommand.id, values, formCommand) : undefined}>
              Same as: {values ? lineOf(formCommand.id, values, formCommand) : "…"}
            </p>
          )
        }
        onSubmit={async (values) => {
          if (!formCommand) throw new Error("Pick a command first.")
          const data = await queue.mutateAsync({ sessionId, command: formCommand.id, args: Object.keys(values).length > 0 ? values : null })
          onQueued?.(data.queueMinionTask)
          setLine("")
          setBack(null)
          setError(null)
        }}
        idPrefix={`task-${sessionId}`}
      />
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

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
import { BrowseButton } from "./browse-button"
import { TaskFilesProvider } from "./task-files"
import { startStreamUpload } from "@/lib/stream-upload"
import { commandOf, lineOf, lineTexts, paramApplies, parseCommandLine, usage, valueCompletion, words, type CommandDef } from "./command-line"
import { CONNECTION_FORMAT, choiceDetail, connectionText, type ConnectionChoice } from "./connection-choices"
import { ConnectionChoicesProvider } from "./session-connection-choices"
import { useSessionConnectionChoices } from "./use-connection-choices"
import { resolveCredentialArgs } from "./console-credentials"
import { commandKindsNote } from "./command-kinds"
import { builtinCompletion, builtinNotice, isBuiltinLine, parseBuiltin } from "./console-builtins"
import { prefersReducedMotion, usePromptFx } from "./prompt-fx"
import type { PromptFxName } from "./prompt-fx-model"

// The console's command line: a command of the session's build typed as a
// line (command-line.ts), with its commands offered as the first word is
// typed, a connection param's values (the session's connections, by name)
// as it is typed, and how the line reads shown under it. Enter runs the line;
// Shift+Enter (or the form button) opens it in the command form, its params
// filled from what was typed (an empty line opens it with a command to
// pick). ↑ and ↓ go through what was run here (the session's tasks, as
// lines), and / anywhere else on the page comes to it. A line that starts
// with / is the console's own (console-builtins.ts), run here, never a task:
// /fx sets the prompt's effect, which plays as keys are typed and lines run
// (prompt-fx.ts).

/** A suggestion's note on a command that works over some kinds only. */
function KindsHint({ kinds, connKind }: { kinds?: readonly string[]; connKind?: string | null }) {
  const note = commandKindsNote(kinds, connKind)
  if (!note) return null
  return (
    <span
      className={cn("ml-auto shrink-0 text-xs", note.blocked ? "text-amber-700 dark:text-amber-400" : "text-muted-foreground")}
      title={note.text}
    >
      {note.blocked ? "switch first" : `over ${(kinds ?? []).join(", ")}`}
    </span>
  )
}

/** The command form's line on a command that works over some kinds only. */
function KindsLine({ kinds, connKind }: { kinds?: readonly string[]; connKind?: string | null }) {
  const note = commandKindsNote(kinds, connKind)
  if (!note) return null
  return <span className={cn("mt-1 block", note.blocked && "text-amber-700 dark:text-amber-400")}>{note.text}.</span>
}

/** A command as the console uses it: its params as the forms know them too. */
export interface ConsoleCommand extends CommandDef {
  fields: readonly FieldDef[]
  /** What its data is, one of core's results (dir_listing), if any. */
  result?: string | null
  /** What core can ask it to do, one of core's actions (list_dir), if any. */
  action?: string | null
  /** The connection kinds it works over; empty: every kind. */
  kinds?: readonly string[]
}

export function CommandBar({
  sessionId,
  operationId,
  commands,
  connKind,
  disabled,
  disabledReason,
  history,
  onQueued,
  effect,
  onEffect,
}: {
  sessionId: string
  /** The session's operation: a file param's file is uploaded to it. */
  operationId: string
  commands: readonly ConsoleCommand[]
  /** The kind of the connection the session's minion uses: a command that does not work over it says so. */
  connKind?: string | null
  disabled: boolean
  /** Why it is disabled, said in its place. */
  disabledReason?: string
  /** The lines ↑ and ↓ go through, oldest first: the session's tasks (commandHistory). */
  history: readonly string[]
  /** A task this queued. */
  onQueued?: (task: MinionTaskFieldsFragment) => void
  /** The prompt's effect, the operator's (null: none). */
  effect: PromptFxName | null
  /** Sets it (/fx). */
  onEffect: (effect: PromptFxName | null) => void
}) {
  const queue = useQueueMinionTask()
  const fx = usePromptFx()
  const connections = useSessionConnectionChoices(sessionId)
  const [line, setLine] = useState("")
  const [pick, setPick] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // What a console command said it did, until the line changes.
  const [notice, setNotice] = useState<string | null>(null)
  // No placeholder right after a line runs with an effect on: the effect
  // plays where the line was (inferno burns its text there). Until a key
  // is typed or the line is left.
  const [hushed, setHushed] = useState(false)
  const [focused, setFocused] = useState(false)
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

  // One of the console's own commands: its words offered, how it reads.
  const builtinLine = isBuiltinLine(line)
  const builtinHint = back === null && builtinLine ? builtinCompletion(line) : null
  const builtin = builtinLine ? parseBuiltin(line, effect) : null
  // The commands the first word could be, while it is being typed.
  // Not while going through the history: ↑ and ↓ are its then.
  const typingCommand = !builtinLine && back === null && words(line).length <= 1 && !/\s$/.test(line)
  const prefix = line.trim().toLowerCase()
  const offered = useMemo(
    () => (typingCommand ? commands.filter((c) => c.id.toLowerCase().startsWith(prefix)) : []),
    [typingCommand, prefix, commands],
  )
  // The values the word being typed could be (a connection param's), past
  // the command; not while going through the history.
  const valueHint = useMemo(
    () => (back === null && !typingCommand && !builtinLine ? valueCompletion(line, commands, connections) : null),
    [back, typingCommand, builtinLine, line, commands, connections],
  )
  const command = builtinLine ? undefined : commandOf(line, commands)
  const parsed = line.trim() === "" || builtinLine ? null : parseCommandLine(line, commands, connections)

  const runBuiltin = (text: string) => {
    const el = input.current
    const b = parseBuiltin(text, effect)
    if (!b.ok) {
      setError(b.error)
      if (el) fx?.refused(el)
      return
    }
    setError(null)
    // Struck while the line is there to measure: by the effect it turns on,
    // once that is up; none when it turns them off.
    if (el && b.run.set !== null) {
      if (b.run.set !== undefined && b.run.set !== effect) fx?.switched(el)
      else fx?.ran(el)
    }
    if (b.run.set !== undefined) onEffect(b.run.set)
    setNotice(builtinNotice(b.run, effect, prefersReducedMotion()))
    setLine("")
    setBack(null)
    setHushed(b.run.set === undefined ? effect !== null : b.run.set !== null)
  }

  const run = (text: string) => {
    if (isBuiltinLine(text)) {
      runBuiltin(text)
      return
    }
    // A file is picked in the form: a line for a command that sends one,
    // without it, opens there.
    const lt = lineTexts(text, commands, connections)
    const c = lt.command ? commands.find((x) => x.id === lt.command!.id) : undefined
    // (A stream needs none: uploaded with curl, the path names it.)
    const file = c?.fields.find((f) => f.type === "file")
    if (file && !lt.texts.has(file.key)) {
      openForm()
      return
    }
    const p = parseCommandLine(text, commands, connections)
    const el = input.current
    if (!p.ok) {
      setError(p.error)
      if (el) fx?.refused(el)
      return
    }
    const fields = c?.fields ?? []
    if (fields.some((f) => (f.type === "credential" || f.type === "credential_list") && f.key in p.args)) {
      // Credentials by name: core says which they are.
      void resolveCredentialArgs(fields, p.args, operationId).then((r) => {
        if (!r.ok) {
          setError(r.error)
          if (input.current) fx?.refused(input.current)
          return
        }
        queueParsed(p.command.id, r.args)
      }, (err: unknown) => setError(errorMessage(err, "Could not look the credential up")))
      return
    }
    queueParsed(p.command.id, p.args)
  }

  const queueParsed = (command: string, args: Record<string, unknown>) => {
    const el = input.current
    setError(null)
    if (el) fx?.ran(el)
    queue.mutate(
      { sessionId, command, args: Object.keys(args).length > 0 ? args : null },
      {
        onSuccess: (data) => {
          onQueued?.(data.queueMinionTask)
          setLine("")
          setBack(null)
          setHushed(effect !== null)
        },
        onError: (err) => setError(errorMessage(err, "Could not queue the task")),
      },
    )
  }

  // A word completed (Tab, Enter or a click on a suggestion): the line
  // becomes next, the word from start to end in it, with its effect.
  const completeWith = (next: string, start: number, end: number) => {
    const el = input.current
    if (el) fx?.completed(el, next, start, end)
    setLine(next)
    setPick(0)
    el?.focus()
  }

  const complete = (c: CommandDef) => completeWith(`${c.id} `, 0, c.id.length)

  // A value picked: the word being typed becomes its name (its place when
  // the name is not its alone).
  const completeValue = (c: ConnectionChoice) => {
    if (!valueHint) return
    const text = connectionText(c.place, connections)
    const word = /\s/.test(text) ? JSON.stringify(text) : text
    completeWith(`${line.slice(0, valueHint.start)}${word} `, valueHint.start, valueHint.start + word.length)
  }

  // A console command's word picked.
  const completeBuiltin = (o: { text: string }) => {
    if (!builtinHint) return
    completeWith(`${line.slice(0, builtinHint.start)}${o.text} `, builtinHint.start, builtinHint.start + o.text.length)
  }

  // Opens the command form on the line: its command's params filled from
  // what was typed for them; an empty line, to pick a command. A console
  // command has no form: it runs.
  const openForm = () => {
    if (isBuiltinLine(line)) {
      runBuiltin(line)
      return
    }
    if (line.trim() === "") {
      setForm({ id: null, picking: true, typed: [], key: nextKey() })
      setFormOpen(true)
      return
    }
    const lt = lineTexts(line, commands, connections)
    const c = lt.command ? commands.find((x) => x.id === lt.command!.id) : undefined
    if (!c) {
      setError(`No command "${words(line)[0].text}": ${commands.map((x) => x.id).join(", ") || "none"}`)
      return
    }
    setError(null)
    setForm({ id: c.id, picking: false, drafts: draftsFromTexts(c.fields, lt.texts), typed: [...lt.texts.keys()], key: nextKey() })
    setFormOpen(true)
  }

  // The suggestions, while the first word could be more than one command.
  const suggesting = offered.length > 0 && prefix !== "" && !(offered.length === 1 && offered[0].id === prefix)
  // A value's, while the word could be more than the one it names already.
  const partial = valueHint?.partial.trim().toLowerCase() ?? ""
  const suggestingBuiltins = builtinHint !== null
  const suggestingValues =
    !suggesting &&
    !suggestingBuiltins &&
    valueHint !== null &&
    valueHint.options.length > 0 &&
    !(valueHint.options.length === 1 && (valueHint.options[0].name.toLowerCase() === partial || String(valueHint.options[0].place) === partial))
  // Nothing while the first word is no command yet: it is being typed.
  const message =
    error ??
    notice ??
    (builtin
      ? builtin.ok
        ? builtin.reads
        : builtin.known
          ? builtin.error
          : null
      : !parsed || !command
        ? null
        : !parsed.ok
          ? parsed.error
          : readsAs(parsed.command, parsed.args, connections))
  const problem = error !== null || (parsed !== null && !parsed.ok) || (builtin !== null && !builtin.ok && builtin.known)

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && e.shiftKey) {
      e.preventDefault()
      openForm()
      return
    }
    if (suggestingBuiltins && builtinHint) {
      const n = builtinHint.options.length
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        setPick((p) => (p + (e.key === "ArrowDown" ? 1 : n - 1)) % n)
        return
      }
      if (e.key === "Tab" || e.key === "Enter") {
        e.preventDefault()
        completeBuiltin(builtinHint.options[Math.min(pick, n - 1)])
        return
      }
    }
    if (suggesting) {
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
    if (suggestingValues && valueHint) {
      const n = valueHint.options.length
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        setPick((p) => (p + (e.key === "ArrowDown" ? 1 : n - 1)) % n)
        return
      }
      if (e.key === "Tab" || e.key === "Enter") {
        e.preventDefault()
        completeValue(valueHint.options[Math.min(pick, n - 1)])
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
      const el = input.current
      // At the oldest already: nothing older, said by the effect alone.
      if (back === 0) {
        if (el) fx?.refused(el)
        return
      }
      if (back === null) draft.current = line
      const i = back === null ? history.length - 1 : back - 1
      if (el) fx?.recalled(el, history[i], true)
      setBack(i)
      setLine(history[i])
      return
    }
    if (e.key === "ArrowDown" && back !== null) {
      e.preventDefault()
      const i = back + 1
      // Past the newest, back to the line that was being typed.
      const next = i >= history.length ? draft.current : history[i]
      const el = input.current
      if (el) fx?.recalled(el, next, false)
      setBack(i >= history.length ? null : i)
      setLine(next)
    }
  }

  if (disabled) {
    // As tall as the line it stands for, said or not.
    return <div className="min-h-10 border-t px-4 py-2.5 text-sm text-muted-foreground">{disabledReason}</div>
  }

  return (
    <div className="relative border-t">
      {/* The commands the first word could be, above the line. */}
      {/* How the line reads, or why it does not: floating above the line,
          as the suggestions do (which it gives way to), so the line keeps
          its place. While it is being typed; an error from a run stays. */}
      {!suggesting && !suggestingValues && !suggestingBuiltins && message && (focused || error !== null) && (
        <div
          role={problem ? "alert" : "status"}
          className={cn(
            "absolute bottom-full left-2 z-20 mb-1 max-w-[calc(100%-1rem)] rounded-md border bg-popover px-3 py-1.5 font-mono text-xs break-words whitespace-pre-wrap shadow-md",
            problem ? "border-red-300 text-red-600 dark:border-red-900 dark:text-red-400" : "text-muted-foreground",
          )}
        >
          {message}
        </div>
      )}
      {suggesting && (
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
              <code className="font-mono text-xs">{usage(c)}</code>
              <span className="truncate text-xs text-muted-foreground">{c.label}</span>
              <KindsHint kinds={c.kinds} connKind={connKind} />
            </button>
          ))}
        </div>
      )}
      {suggestingBuiltins && builtinHint && (
        <div className="absolute inset-x-2 bottom-full z-20 mb-1 overflow-hidden rounded-md border bg-popover shadow-md" role="listbox">
          {builtinHint.options.map((o, i) => (
            <button
              key={o.text}
              type="button"
              role="option"
              aria-selected={i === pick}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => completeBuiltin(o)}
              className={cn("flex w-full cursor-pointer items-baseline gap-3 px-3 py-1.5 text-left text-sm", i === pick && "bg-muted")}
            >
              <code className="font-mono text-xs">{o.text}</code>
              <span className="truncate text-xs text-muted-foreground">{o.label}</span>
            </button>
          ))}
        </div>
      )}
      {suggestingValues && valueHint && (
        <div className="absolute inset-x-2 bottom-full z-20 mb-1 overflow-hidden rounded-md border bg-popover shadow-md" role="listbox">
          {valueHint.options.map((c, i) => (
            <button
              key={c.place}
              type="button"
              role="option"
              aria-selected={i === pick}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => completeValue(c)}
              className={cn("flex w-full cursor-pointer items-baseline gap-3 px-3 py-1.5 text-left text-sm", i === pick && "bg-muted")}
            >
              <code className="font-mono text-xs">{c.name}</code>
              <span className="truncate text-xs text-muted-foreground">{choiceDetail(c)}</span>
              {c.inUse && <span className="ml-auto shrink-0 text-xs text-muted-foreground">in use</span>}
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
            setNotice(null)
            setHushed(false)
            setBack(null)
            setPick(0)
            // A key typed or deleted; text pasted or cut, dropped, undone.
            fx?.edited(e.target, line)
          }}
          onKeyDown={onKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => {
            setFocused(false)
            setHushed(false)
          }}
          spellCheck={false}
          autoComplete="off"
          aria-label="Command"
          placeholder={hushed ? "" : commands.length > 0 ? `${commands.map((c) => c.id).join(", ")}` : "No commands"}
          title="The command line (/). A line that starts with / is the console's own: /fx sets the prompt's effect."
          className="peer min-w-0 flex-1 bg-transparent font-mono text-xs outline-none placeholder:text-muted-foreground/60"
        />
        <kbd className="rounded border px-1.5 font-mono text-xs text-muted-foreground peer-focus:hidden" title="Press / to type a command">
          /
        </kbd>
        <span className="hidden shrink-0 items-center gap-1 text-xs text-muted-foreground peer-focus:inline-flex">
          <kbd className="rounded border px-1 font-mono">↵</kbd> run
          <kbd className="ml-1 rounded border px-1 font-mono">⇧↵</kbd> form
        </span>
        <BrowseButton sessionId={sessionId} commands={commands} onQueued={onQueued} />
        <Button variant="ghost" size="icon-xs" title="Fill the command's params in a form (Shift+Enter)" aria-label="Command form"
          disabled={commands.length === 0} onMouseDown={(e) => e.preventDefault()} onClick={openForm}>
          <SlidersHorizontalIcon />
        </Button>
        <Button size="xs" disabled={queue.isPending || line.trim() === ""} onClick={() => run(line)}>
          <PlayIcon />
          Run
        </Button>
      </div>
      <TaskFilesProvider key={`${form?.key}-${form?.id}`} operationId={operationId}>
        <ConnectionChoicesProvider choices={connections}>
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
                <KindsLine kinds={formCommand.kinds} connKind={connKind} />
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
              <p
                className="truncate font-mono text-xs text-muted-foreground"
                title={values ? lineOf(formCommand.id, values, formCommand, connections) : undefined}
              >
                Same as: {values ? lineOf(formCommand.id, values, formCommand, connections) : "…"}
              </p>
            )
          }
          onSubmit={async (values, drafts) => {
            if (!formCommand) throw new Error("Pick a command first.")
            const data = await queue.mutateAsync({ sessionId, command: formCommand.id, args: Object.keys(values).length > 0 ? values : null })
            // A stream picked here goes now, as the task waits for it.
            for (const d of Object.values(drafts)) {
              if (d.kind === "stream" && d.file) startStreamUpload(data.queueMinionTask.id, d.file)
            }
            onQueued?.(data.queueMinionTask)
            setLine("")
            setBack(null)
            setError(null)
          }}
          idPrefix={`task-${sessionId}`}
        />
        </ConnectionChoicesProvider>
      </TaskFilesProvider>

    </div>
  )
}

/** How a parsed line reads: each param's value that applies, defaults marked; a connection's with its name. */
function readsAs(command: CommandDef, args: Record<string, unknown>, connections: readonly ConnectionChoice[]): string {
  const parts = command.params.map((p) => {
    if (!paramApplies(p, command.params, args)) return null
    if (p.key in args && p.format === CONNECTION_FORMAT) {
      const c = connections.find((x) => x.place === args[p.key])
      return `${p.key}=${JSON.stringify(args[p.key])}${c ? ` (${c.name}, ${choiceDetail(c)}${c.inUse ? ", in use" : ""})` : ""}`
    }
    if (p.key in args) return `${p.key}=${JSON.stringify(args[p.key])}`
    if (p.default !== null && p.default !== undefined) return `${p.key}=${JSON.stringify(p.default)} (default)`
    return null
  })
  return [command.id, ...parts.filter((p) => p !== null)].join("  ")
}

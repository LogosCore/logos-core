import { PROMPT_FX, readPromptFx, type PromptFxName } from "./prompt-fx-model"

// The console's own commands: a line that starts with "/" is one, run in
// the browser for the operator typing it. It never queues a task, so no
// minion hears of it and it is in no session's history; a build's command
// id starts with a letter (the protocol's), so none can be one. The first:
// /fx, the prompt's effect (prompt-fx-model.ts), kept with the operator's
// sessions layout: theirs, in every session.

export interface ConsoleBuiltin {
  id: string
  /** How it is typed. */
  usage: string
  label: string
  /** What its one word can be, offered as it is typed. */
  values: readonly { value: string; label: string }[]
}

/** /fx's word for no effect. */
export const FX_OFF = "off"

export const CONSOLE_BUILTINS: readonly ConsoleBuiltin[] = [
  {
    id: "fx",
    usage: `/fx [${[...PROMPT_FX.map((f) => f.name), FX_OFF].join("|")}]`,
    label: "The prompt's effect: yours, in every session",
    values: [...PROMPT_FX.map((f) => ({ value: f.name, label: f.description })), { value: FX_OFF, label: "no effect" }],
  },
]

/** Whether a line is one of the console's own commands. */
export function isBuiltinLine(line: string): boolean {
  return line.trimStart().startsWith("/")
}

/** What a builtin line does. /fx: set is the effect it turns on (null: off), undefined when it only shows the one on. */
export type BuiltinRun = { id: "fx"; set: PromptFxName | null | undefined }

export type ParsedBuiltin =
  | { ok: true; run: BuiltinRun; /** How it reads, while it is typed. */ reads: string }
  | { ok: false; error: string; /** Whether its first word names one: else it may be being typed yet. */ known: boolean }

const fxChoices = () => [...PROMPT_FX.map((f) => f.name), FX_OFF].join(", ")

/** A builtin line read; current: the effect on. */
export function parseBuiltin(line: string, current: PromptFxName | null): ParsedBuiltin {
  const [first = "", ...rest] = line.trim().slice(1).split(/\s+/).filter(Boolean)
  const id = first.toLowerCase()
  if (!CONSOLE_BUILTINS.some((b) => b.id === id)) {
    return { ok: false, error: `No console command "/${first}": ${CONSOLE_BUILTINS.map((b) => `/${b.id}`).join(", ")}`, known: false }
  }
  if (rest.length === 0) {
    return { ok: true, run: { id: "fx", set: undefined }, reads: `/fx  shows the prompt's effect: ${current ?? FX_OFF}` }
  }
  if (rest.length > 1) return { ok: false, error: `/fx takes one word: ${fxChoices()}`, known: true }
  const word = rest[0].toLowerCase()
  if (word === FX_OFF) return { ok: true, run: { id: "fx", set: null }, reads: "/fx  off: no effect" }
  const fx = readPromptFx(word)
  if (!fx) return { ok: false, error: `No effect "${rest[0]}": ${fxChoices()}`, known: true }
  const f = PROMPT_FX.find((x) => x.name === fx)!
  return { ok: true, run: { id: "fx", set: fx }, reads: `/fx  ${fx}: ${f.description}` }
}

/** What a builtin said it did, run: current was the effect on, reduced whether the system asks for less motion. */
export function builtinNotice(run: BuiltinRun, current: PromptFxName | null, reduced: boolean): string {
  const fx = run.set === undefined ? current : run.set
  const said =
    run.set === undefined
      ? fx
        ? `The prompt's effect: ${fx}. /fx ${FX_OFF} turns it off.`
        : `The prompt has no effect. /fx ${PROMPT_FX[0].name} turns ${PROMPT_FX[0].name} on.`
      : fx
        ? `The prompt's effect: ${fx}, yours in every session.`
        : "The prompt has no effect now."
  return fx && reduced ? `${said} Your system asks for less motion: none shows here.` : said
}

/** A word the line could take where it is being typed: start, where that word starts. */
export interface BuiltinOffer {
  start: number
  options: readonly { text: string; label: string }[]
}

/**
 * What the word being typed on a builtin line could be: a builtin, while
 * the first is typed; one of its values, after it. Nothing when it is the
 * only one already, or none fits.
 */
export function builtinCompletion(line: string): BuiltinOffer | null {
  const first = /^(\s*)\/(\S*)$/.exec(line)
  if (first) {
    const typed = first[2].toLowerCase()
    const options = CONSOLE_BUILTINS.filter((b) => b.id.startsWith(typed)).map((b) => ({ text: `/${b.id}`, label: b.label }))
    return offer(first[1].length, options, `/${typed}`)
  }
  const value = /^(\s*\/(\S+)\s+)(\S*)$/.exec(line)
  if (value) {
    const b = CONSOLE_BUILTINS.find((x) => x.id === value[2].toLowerCase())
    if (!b) return null
    const typed = value[3].toLowerCase()
    const options = b.values.filter((v) => v.value.startsWith(typed)).map((v) => ({ text: v.value, label: v.label }))
    return offer(value[1].length, options, typed)
  }
  return null
}

function offer(start: number, options: BuiltinOffer["options"], typed: string): BuiltinOffer | null {
  if (options.length === 0 || (options.length === 1 && options[0].text === typed)) return null
  return { start, options }
}

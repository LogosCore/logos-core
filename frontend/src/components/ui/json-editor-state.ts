import type { CompletionContext, Completion, CompletionResult } from "@codemirror/autocomplete"
import { ensureSyntaxTree, foldable, foldEffect, syntaxTree } from "@codemirror/language"
import type { Diagnostic } from "@codemirror/lint"
import { EditorState, StateEffect, StateField, type TransactionSpec } from "@codemirror/state"
import type { SyntaxNode } from "@lezer/common"
import { formatJson, hintsAt, parseJson, type JsonHints, type JsonProblem } from "@/lib/json-edit"
import { firstErrorAt, keyOf, keysOf, pointerOfNode, propertySpans, type PropertySpan } from "./json-editor-tree"

// The JSON editor's state (json-editor.tsx) that needs no page: what the
// record said (locks, hints), the spans of the text's properties, the filter
// that keeps locked properties as they are, the marks for problems, and what
// to offer as you type. Apart from the view so it can be tested on its own.

const NONE: readonly never[] = []
const NO_HINTS: JsonHints = {}

// --- What the record said: locks and hints ---------------------------------

export interface Config {
  locked: readonly string[]
  hints: JsonHints
}

export const setConfig = StateEffect.define<Config>()

export const config = StateField.define<Config>({
  create: () => ({ locked: NONE, hints: NO_HINTS }),
  update: (value, tr) => {
    for (const e of tr.effects) if (e.is(setConfig)) return e.value
    return value
  },
})

/** Every property's span, kept with the text. Documents here are small: parsed whole. */
export const spans = StateField.define<Map<string, PropertySpan>>({
  create: (state) => spansOf(state),
  update: (value, tr) => (tr.docChanged ? spansOf(tr.state) : value),
})

function spansOf(state: EditorState): Map<string, PropertySpan> {
  const tree = ensureSyntaxTree(state, state.doc.length, 200) ?? syntaxTree(state)
  return propertySpans(tree, state.doc.toString())
}

export function lockedSpans(state: EditorState): PropertySpan[] {
  const all = state.field(spans)
  return state.field(config).locked.flatMap((p) => {
    const span = p === "" ? undefined : all.get(p)
    return span ? [span] : []
  })
}

// Typing into a locked property, or deleting across one, is refused. Replacing
// the whole document is not: pasting a record's export over it must work, and
// the record checks the locked values when it reads the text back.
export const keepLocks = EditorState.changeFilter.of((tr) => {
  if (!tr.docChanged) return true
  const length = tr.startState.doc.length
  let whole = false
  tr.changes.iterChangedRanges((fromA, toA) => {
    if (fromA === 0 && toA === length) whole = true
  })
  if (whole) return true
  const locked = lockedSpans(tr.startState)
  let touches = false
  tr.changes.iterChangedRanges((fromA, toA) => {
    for (const s of locked) {
      if (fromA === toA ? fromA > s.from && fromA < s.to : fromA < s.to && toA > s.from) touches = true
    }
  })
  return !touches
})

// --- Problems ---------------------------------------------------------------

export function diagnosticsOf(state: EditorState, problems: readonly JsonProblem[]): Diagnostic[] {
  const text = state.doc.toString()
  const parsed = parseJson(text)
  if (!parsed.ok) {
    const tree = ensureSyntaxTree(state, state.doc.length, 200) ?? syntaxTree(state)
    const from = Math.min(firstErrorAt(tree) ?? positionIn(parsed.message, state) ?? 0, text.length)
    return [{ from, to: Math.min(from + 1, text.length), severity: "error", message: `Not JSON: ${parsed.message}` }]
  }
  const all = state.field(spans)
  return problems.map((p) => {
    const found = all.get(p.path)
    if (found) {
      // A plain value is marked where it is wrong; an object or array at its key.
      const scalar = found.valueType !== "Object" && found.valueType !== "Array" && found.valueType !== ""
      const [from, to] = scalar ? [found.valueFrom, found.valueTo] : [found.keyFrom, found.keyTo]
      return { from, to: Math.max(to, from + 1), severity: "error" as const, message: p.message }
    }
    // Not in the text (a missing key): on the nearest part that is, naming it.
    const { span, missing } = nearest(all, p.path)
    const from = span.keyTo > span.keyFrom ? span.keyFrom : span.from
    const to = span.keyTo > span.keyFrom ? span.keyTo : Math.min(span.from + 1, text.length)
    return { from, to, severity: "error" as const, message: missing ? `${missing}: ${p.message}` : p.message }
  })
}

function nearest(all: Map<string, PropertySpan>, path: string): { span: PropertySpan; missing: string } {
  let at = path
  let missing = ""
  while (at !== "") {
    const cut = at.lastIndexOf("/")
    if (!missing) missing = decodeToken(at.slice(cut + 1))
    at = at.slice(0, cut)
    const span = all.get(at)
    if (span) return { span, missing }
  }
  return { span: all.get("") ?? { from: 0, to: 0, keyFrom: 0, keyTo: 0, valueFrom: 0, valueTo: 0, valueType: "" }, missing }
}

function decodeToken(token: string): string {
  return token.replaceAll("~1", "/").replaceAll("~0", "~")
}

/** A syntax error's place from the browser's message, where the tree had none. */
function positionIn(message: string, state: EditorState): number | null {
  const at = /position (\d+)/.exec(message)
  if (at) return Number(at[1])
  const lineCol = /line (\d+) column (\d+)/.exec(message)
  if (lineCol) {
    const line = Number(lineCol[1])
    if (line < 1 || line > state.doc.lines) return null
    return state.doc.line(line).from + Number(lineCol[2]) - 1
  }
  return null
}

// --- Completion -------------------------------------------------------------

/**
 * The object a key typed at pos goes in, the text that key replaces, and
 * whether a colon follows it already (renaming a key, rather than adding one).
 */
function keyContext(
  context: CompletionContext,
): { object: SyntaxNode; from: number; to: number; colon: boolean } | null {
  const { state, pos } = context
  const node = syntaxTree(state).resolveInner(pos, -1)
  if (node.name === "PropertyName") {
    const object = node.parent?.parent
    const colon = node.parent?.getChild(":") != null
    return object?.name === "Object" ? { object, from: node.from, to: node.to, colon } : null
  }
  // Between an object's properties: right in it, or in what the parser could
  // not make sense of there (a trailing comma, a key being typed).
  const between =
    node.type.isError || node.name === "{" || node.name === ","
      ? node.parent?.name === "Object"
        ? node.parent
        : null
      : node.name === "Object"
        ? node
        : null
  if (!between) return null
  // A key being typed: an unclosed string, where a key can start.
  const typed = context.matchBefore(/"[^"\\]*$/)
  if (typed && startsKey(state, between, typed.from)) return { object: between, from: typed.from, to: pos, colon: false }
  if (startsKey(state, between, pos)) return { object: between, from: pos, to: pos, colon: false }
  return null
}

/** Whether a key can start at pos in object: after its "{", or after a ",". */
function startsKey(state: EditorState, object: SyntaxNode, pos: number): boolean {
  const before = state.sliceDoc(object.from, pos).trimEnd()
  return before.endsWith("{") || before.endsWith(",")
}

export function completeJson(context: CompletionContext): CompletionResult | null {
  const { state, pos } = context
  const { hints } = state.field(config)
  const doc = state.doc.toString()

  const key = keyContext(context)
  if (key) {
    const pointer = pointerOfNode(key.object, doc)
    const offered = pointer === null ? undefined : hintsAt(hints, pointer)
    if (!offered?.length) return null
    // Keys the object has are not offered again, unless one is being renamed.
    const present = keysOf(key.object, doc)
    const options: Completion[] = offered
      .filter((h) => key.colon || !present.has(h.key))
      .map((h) => ({
        label: JSON.stringify(h.key),
        detail: h.detail,
        info: h.info,
        type: "property",
        apply: key.colon ? JSON.stringify(h.key) : `${JSON.stringify(h.key)}: `,
      }))
    return { from: key.from, to: key.to, options, validFor: /^"?[^"\\]*"?$/ }
  }

  // A value of a key that has values to offer.
  const node = syntaxTree(state).resolveInner(pos, -1)
  const item = itemContext(context, node)
  if (item) return item
  const property = node.name === "Property" ? node : node.parent?.name === "Property" ? node.parent : null
  const name = property?.getChild("PropertyName")
  if (!property || !name || pos <= name.to) return null
  const pointer = pointerOfNode(property.parent!, doc)
  const hint = pointer === null ? undefined : hintsAt(hints, pointer)?.find((h) => h.key === keyOf(name, doc))
  if (!hint?.values?.length) return null
  const valueNode = node.name === "Property" ? null : node
  const from = valueNode && valueNode !== name ? valueNode.from : pos
  const to = valueNode && valueNode !== name ? valueNode.to : pos
  return {
    from,
    to,
    options: hint.values.map((v) => ({ label: JSON.stringify(v), type: v === null ? "keyword" : "constant" })),
  }
}

/**
 * An item of a list whose key offers items: those the list does not hold
 * yet, in place of the item there (one being typed, say), or between items.
 */
function itemContext(context: CompletionContext, node: SyntaxNode): CompletionResult | null {
  const { state, pos } = context
  // In the list, or in what the parser could not make sense of there (an
  // item being typed without its closing quote).
  const at = node.type.isError && node.parent?.name === "Array" ? node.parent : node
  const inArray = at === node && node.parent?.name === "Array"
  const array = at.name === "Array" ? at : inArray ? node.parent : null
  const property = array?.parent
  const name = property?.name === "Property" ? property.getChild("PropertyName") : null
  if (!array || !property || !name) return null
  const doc = state.doc.toString()
  const pointer = pointerOfNode(property.parent!, doc)
  const items = pointer === null ? undefined : hintsAt(state.field(config).hints, pointer)?.find((h) => h.key === keyOf(name, doc))?.items
  if (!items?.length) return null
  // An item written out is replaced; one being typed too.
  const current = inArray && VALUE_ITEMS.has(node.name) && pos > node.from ? node : null
  // Typed text starts an item only after "[" or ","; a quote before it may
  // close the item before.
  const match = current ? null : context.matchBefore(/"[^"\\]*$|[\w-]+$/)
  const typed = match && /[[,]$/.test(state.sliceDoc(array.from, match.from).trimEnd()) ? match : null
  const listed = new Set<string>()
  for (let c = array.firstChild; c; c = c.nextSibling) {
    if (c !== current && VALUE_ITEMS.has(c.name)) listed.add(doc.slice(c.from, c.to))
  }
  const options: Completion[] = items
    .filter((i) => !listed.has(JSON.stringify(i.value)))
    .map((i) => ({ label: JSON.stringify(i.value), detail: i.detail, info: i.info, type: "constant" }))
  if (options.length === 0) return null
  return { from: current?.from ?? typed?.from ?? pos, to: current?.to ?? pos, options, validFor: /^"?[^"\\]*"?$/ }
}

const VALUE_ITEMS = new Set(["String", "Number", "True", "False", "Null"])

// --- Commands --------------------------------------------------------------

/**
 * The document indented two spaces, as the editors show one: after pasting
 * minified JSON, say. Null when it is not JSON or already so. It replaces
 * the whole text, which the locks let through; the values stay the same.
 */
export function formatSpec(state: EditorState): TransactionSpec | null {
  const text = state.doc.toString()
  const parsed = parseJson(text)
  if (!parsed.ok) return null
  const formatted = formatJson(parsed.value)
  if (formatted === text) return null
  return { changes: { from: 0, to: text.length, insert: formatted }, userEvent: "input.format" }
}

/**
 * Folds every part inside the document's top value, each to one line, and
 * leaves the top value open, so its keys stay in view. (CodeMirror's foldAll
 * folds the top value itself, which leaves "{…}".)
 */
export function foldContentsEffects(state: EditorState): StateEffect<{ from: number; to: number }>[] {
  const effects: StateEffect<{ from: number; to: number }>[] = []
  const first = state.doc.line(1)
  const top = foldable(state, first.from, first.to)
  const end = top ? top.to : state.doc.length
  let pos = top ? first.to + 1 : 0
  while (pos < end) {
    const line = state.doc.lineAt(pos)
    const range = foldable(state, line.from, line.to)
    if (range && range.to <= end) effects.push(foldEffect.of(range))
    pos = (range ? state.doc.lineAt(range.to) : line).to + 1
  }
  return effects
}

// --- Text from outside -------------------------------------------------------

/** The change from a to b: the part between what they start and end with alike. */
export function changedPart(a: string, b: string): { from: number; to: number; insert: string } {
  let start = 0
  const shorter = Math.min(a.length, b.length)
  while (start < shorter && a.charCodeAt(start) === b.charCodeAt(start)) start++
  let endA = a.length
  let endB = b.length
  while (endA > start && endB > start && a.charCodeAt(endA - 1) === b.charCodeAt(endB - 1)) {
    endA--
    endB--
  }
  return { from: start, to: endA, insert: b.slice(start, endB) }
}

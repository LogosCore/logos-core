import { useEffect, useRef } from "react"
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete"
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands"
import { json } from "@codemirror/lang-json"
import {
  bracketMatching,
  foldGutter,
  foldKeymap,
  HighlightStyle,
  indentOnInput,
  indentUnit,
  syntaxHighlighting,
  syntaxTree,
} from "@codemirror/language"
import { lintGutter, lintKeymap, setDiagnostics } from "@codemirror/lint"
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search"
import { Compartment, EditorState, type Extension } from "@codemirror/state"
import {
  Decoration,
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  hoverTooltip,
  keymap,
  lineNumbers,
  WidgetType,
} from "@codemirror/view"
import { tags as t } from "@lezer/highlight"
import { cn } from "@/lib/utils"
import type { JsonHints, JsonKeyHint, JsonProblem } from "@/lib/json-edit"
import { keyOf, pointerOfNode } from "./json-editor-tree"
import { completeJson, config, diagnosticsOf, keepLocks, lockedSpans, setConfig, spans } from "./json-editor-state"
import "./json-editor.css"

// A JSON document editor for a record's JSON tab, built on CodeMirror. It
// knows nothing of the record: the record says which parts are locked, where
// its problems are and what to offer as you type (lib/json-edit.ts), and
// reads the text back itself. Load it through lazy-json-editor.tsx: it is
// heavy, and most screens never show it.

export interface JsonEditorProps {
  /** The document's text. */
  value: string
  onChange: (text: string) => void
  /**
   * Properties shown but not changed (JSON Pointers, "/format"). They cannot
   * be typed into; replacing the whole document still works, so a record
   * should check them when it reads the text back.
   */
  locked?: readonly string[]
  /** Problems to mark in the text, by JSON Pointer, besides syntax errors. */
  problems?: readonly JsonProblem[]
  /** Keys and values to offer, by the pointer of the object they go in. */
  hints?: JsonHints
  readOnly?: boolean
  className?: string
  "aria-label"?: string
}

const NONE: readonly never[] = []
const NO_HINTS: JsonHints = {}
/** Whether the text can be changed; a key, so one serves every editor. */
const editable = new Compartment()

export function JsonEditor({
  value,
  onChange,
  locked = NONE,
  problems = NONE,
  hints = NO_HINTS,
  readOnly = false,
  className,
  "aria-label": ariaLabel = "JSON",
}: JsonEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const changed = useRef(onChange)
  const first = useRef({ value, ariaLabel })

  useEffect(() => {
    changed.current = onChange
  })

  useEffect(() => {
    const v = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: first.current.value,
        extensions: [
          base,
          editable.of([]),
          EditorView.contentAttributes.of({ "aria-label": first.current.ariaLabel }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) changed.current(u.state.doc.toString())
          }),
        ],
      }),
    })
    view.current = v
    return () => {
      v.destroy()
      view.current = null
    }
  }, [])

  // A new value from outside (the form, a reset): replace the text, past the locks.
  useEffect(() => {
    const v = view.current
    if (!v) return
    const text = v.state.doc.toString()
    if (text !== value) v.dispatch({ changes: { from: 0, to: text.length, insert: value }, filter: false })
  }, [value])

  useEffect(() => {
    view.current?.dispatch({ effects: setConfig.of({ locked, hints }) })
  }, [locked, hints])

  useEffect(() => {
    view.current?.dispatch({
      effects: editable.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
    })
  }, [readOnly])

  // After the text is in: marks are placed on the text they are about.
  useEffect(() => {
    const v = view.current
    if (v) v.dispatch(setDiagnostics(v.state, diagnosticsOf(v.state, problems)))
  }, [value, problems])

  return <div ref={host} className={cn("json-editor min-h-0 overflow-hidden", className)} />
}

class LockWidget extends WidgetType {
  eq() {
    return true
  }
  toDOM() {
    const el = document.createElement("span")
    el.className = "cm-json-lock"
    el.title = "Locked: it stays as it is"
    el.setAttribute("aria-label", "locked")
    el.innerHTML = LOCK_SVG
    return el
  }
}

// lucide's "lock", at the text's size.
const LOCK_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>'

const lockMark = Decoration.mark({ class: "cm-json-locked" })
const lockWidget = Decoration.widget({ widget: new LockWidget(), side: -1 })

const lockDecorations = EditorView.decorations.compute([spans, config], (state) =>
  Decoration.set(
    lockedSpans(state).flatMap((s) => [lockWidget.range(s.from), lockMark.range(s.from, s.to)]),
    true,
  ),
)

// --- Completion and hover --------------------------------------------------

const hintHover = hoverTooltip((view, pos) => {
  const node = syntaxTree(view.state).resolveInner(pos, 1)
  const object = node.parent?.parent
  if (node.name !== "PropertyName" || !object || object.name !== "Object") return null
  const doc = view.state.doc.toString()
  const pointer = pointerOfNode(object, doc)
  const hint: JsonKeyHint | undefined = pointer === null ? undefined : view.state.field(config).hints[pointer]?.find((h) => h.key === keyOf(node, doc))
  if (!hint?.info && !hint?.detail) return null
  return {
    pos: node.from,
    end: node.to,
    above: true,
    create: () => {
      const dom = document.createElement("div")
      dom.className = "cm-json-hint"
      const title = dom.appendChild(document.createElement("div"))
      title.className = "cm-json-hint-title"
      title.textContent = hint.detail ? `${hint.key} · ${hint.detail}` : hint.key
      if (hint.info) dom.appendChild(document.createElement("div")).textContent = hint.info
      return { dom }
    },
  }
})

// --- Look ----------------------------------------------------------------------

const highlight = HighlightStyle.define([
  { tag: t.propertyName, color: "var(--json-key)" },
  { tag: t.string, color: "var(--json-string)" },
  { tag: t.number, color: "var(--json-number)" },
  { tag: [t.bool, t.null], color: "var(--json-literal)" },
  { tag: [t.separator, t.brace, t.squareBracket], color: "var(--muted-foreground)" },
])

const theme = EditorView.theme({
  "&": { height: "100%", fontSize: "13px", color: "var(--foreground)", backgroundColor: "transparent" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "1.6", overflow: "auto" },
  ".cm-content": { caretColor: "var(--foreground)", paddingBlock: "8px" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--foreground)" },
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection":
    { backgroundColor: "color-mix(in oklab, var(--json-key) 22%, transparent)" },
  ".cm-gutters": { backgroundColor: "transparent", color: "var(--muted-foreground)", border: "none" },
  ".cm-activeLine": { backgroundColor: "color-mix(in oklab, var(--muted) 55%, transparent)" },
  ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--foreground)" },
  ".cm-matchingBracket": { backgroundColor: "color-mix(in oklab, var(--json-key) 18%, transparent)", outline: "none" },
  ".cm-tooltip": {
    backgroundColor: "var(--popover)",
    color: "var(--popover-foreground)",
    border: "1px solid var(--border)",
    borderRadius: "6px",
    boxShadow: "0 4px 12px rgb(0 0 0 / 0.12)",
  },
  ".cm-tooltip-autocomplete ul li[aria-selected]": { backgroundColor: "var(--accent)", color: "var(--accent-foreground)" },
  ".cm-completionDetail": { color: "var(--muted-foreground)", fontStyle: "normal", marginLeft: "0.75em" },
  ".cm-completionInfo": { whiteSpace: "pre-line", maxWidth: "24rem" },
  ".cm-diagnostic": { whiteSpace: "pre-line" },
})

const base: Extension = [
  json(),
  config,
  spans,
  lineNumbers(),
  highlightActiveLineGutter(),
  foldGutter(),
  lintGutter(),
  history(),
  drawSelection(),
  indentOnInput(),
  indentUnit.of("  "),
  EditorState.tabSize.of(2),
  bracketMatching(),
  closeBrackets(),
  highlightActiveLine(),
  highlightSelectionMatches(),
  EditorView.lineWrapping,
  syntaxHighlighting(highlight),
  autocompletion({ override: [completeJson] }),
  hintHover,
  keepLocks,
  lockDecorations,
  keymap.of([
    ...closeBracketsKeymap,
    ...defaultKeymap,
    ...searchKeymap,
    ...historyKeymap,
    ...foldKeymap,
    ...completionKeymap,
    ...lintKeymap,
    indentWithTab,
  ]),
  theme,
]

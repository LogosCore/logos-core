import { useEffect, useRef, useState, type ReactNode } from "react"
import {
  CheckIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  CircleAlertIcon,
  CopyIcon,
  DownloadIcon,
  Redo2Icon,
  SearchIcon,
  TextQuoteIcon,
  Undo2Icon,
  WrapTextIcon,
} from "lucide-react"
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete"
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
  redo,
  redoDepth,
  undo,
  undoDepth,
} from "@codemirror/commands"
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
  unfoldAll,
} from "@codemirror/language"
import { lintGutter, lintKeymap, nextDiagnostic, setDiagnostics } from "@codemirror/lint"
import { highlightSelectionMatches, openSearchPanel, search, searchKeymap } from "@codemirror/search"
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
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { downloadBlob } from "@/lib/file-export"
import { cn } from "@/lib/utils"
import { hintsAt, parseJson, type JsonHints, type JsonKeyHint, type JsonProblem } from "@/lib/json-edit"
import { keyOf, pointerOfNode } from "./json-editor-tree"
import {
  completeJson,
  config,
  diagnosticsOf,
  foldContentsEffects,
  formatSpec,
  keepLocks,
  lockedSpans,
  setConfig,
  spans,
} from "./json-editor-state"
import "./json-editor.css"

// A JSON document editor for a record's JSON tab, built on CodeMirror. It
// knows nothing of the record: the record says which parts are locked, where
// its problems are and what to offer as you type (lib/json-edit.ts), and
// reads the text back itself. A toolbar above the text: undo and redo,
// format, fold and unfold all, wrap lines; the problems, find, copy all and
// download. Load it through lazy-json-editor.tsx: it is heavy, and most
// screens never show it.

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
  /** The name Download saves the text under; without one, no Download. */
  fileName?: string
  className?: string
  "aria-label"?: string
}

const NONE: readonly never[] = []
const NO_HINTS: JsonHints = {}
/** Whether the text can be changed; a key, so one serves every editor. */
const editable = new Compartment()
/** Whether long lines wrap. */
const wrapping = new Compartment()

const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)
const MOD = IS_MAC ? "⌘" : "Ctrl+"
const ALT = IS_MAC ? "⌥" : "Alt+"
const SHIFT = IS_MAC ? "⇧" : "Shift+"
const CTRL = IS_MAC ? "⌃" : "Ctrl+"

export function JsonEditor({
  value,
  onChange,
  locked = NONE,
  problems = NONE,
  hints = NO_HINTS,
  readOnly = false,
  fileName,
  className,
  "aria-label": ariaLabel = "JSON",
}: JsonEditorProps) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView | null>(null)
  const changed = useRef(onChange)
  const first = useRef({ value, ariaLabel })
  // What the toolbar shows that only the editor knows.
  const [canUndo, setCanUndo] = useState(false)
  const [canRedo, setCanRedo] = useState(false)
  const [wrap, setWrap] = useState(true)

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
          wrapping.of(EditorView.lineWrapping),
          EditorView.contentAttributes.of({ "aria-label": first.current.ariaLabel }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) changed.current(u.state.doc.toString())
            setCanUndo(undoDepth(u.state) > 0)
            setCanRedo(redoDepth(u.state) > 0)
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

  useEffect(() => {
    view.current?.dispatch({ effects: wrapping.reconfigure(wrap ? EditorView.lineWrapping : []) })
  }, [wrap])

  /**
   * Runs an editor command from the toolbar, then hands the keyboard back to
   * the text; not after Find, whose panel takes it.
   */
  function run(command: (v: EditorView) => boolean, focus = true) {
    const v = view.current
    if (!v) return
    command(v)
    if (focus) v.focus()
  }
  const text = () => view.current?.state.doc.toString() ?? value
  const parses = parseJson(value).ok
  const problemCount = parses ? problems.length : 1

  return (
    <div className={cn("json-editor flex min-h-0 flex-col overflow-hidden", className)}>
      <div role="toolbar" aria-label="JSON tools" className="flex shrink-0 flex-wrap items-center gap-0.5 border-b bg-muted/40 p-1">
        <Tool label="Undo" keys={`${MOD}Z`} onClick={() => run(undo)} disabled={readOnly || !canUndo}>
          <Undo2Icon />
        </Tool>
        <Tool label="Redo" keys={IS_MAC ? "⌘⇧Z" : "Ctrl+Y"} onClick={() => run(redo)} disabled={readOnly || !canRedo}>
          <Redo2Icon />
        </Tool>
        <span aria-hidden className="mx-1 h-4 w-px bg-border" />
        <Tool label="Format" keys={`${SHIFT}${ALT}F`} onClick={() => run(formatCommand)} disabled={readOnly || !parses}>
          <TextQuoteIcon />
        </Tool>
        <Tool label="Fold all" keys={`${CTRL}${ALT}[`} onClick={() => run(foldContents)}>
          <ChevronsDownUpIcon />
        </Tool>
        <Tool label="Unfold all" keys={`${CTRL}${ALT}]`} onClick={() => run(unfoldAll)}>
          <ChevronsUpDownIcon />
        </Tool>
        <Tool label="Wrap long lines" onClick={() => setWrap((w) => !w)} pressed={wrap}>
          <WrapTextIcon />
        </Tool>

        <span className="flex-1" />
        {problemCount > 0 ? (
          <Tool label="Go to the next problem" keys="F8" onClick={() => run(nextDiagnostic)} wide>
            <span className="flex items-center gap-1 text-destructive">
              <CircleAlertIcon />
              {problemCount === 1 ? "1 problem" : `${problemCount} problems`}
            </span>
          </Tool>
        ) : (
          <span className="flex items-center gap-1 px-2 text-xs text-muted-foreground">
            <CheckIcon className="size-3.5" />
            No problems
          </span>
        )}
        <Tool label="Find" keys={`${MOD}F`} onClick={() => run(openSearchPanel, false)}>
          <SearchIcon />
        </Tool>
        <Tool label="Copy all" onClick={() => void copyToClipboard(text(), "JSON")}>
          <CopyIcon />
        </Tool>
        {fileName && (
          <Tool label={`Download as ${fileName}`} onClick={() => downloadBlob(withNewline(text()), fileName, "application/json")}>
            <DownloadIcon />
          </Tool>
        )}
      </div>
      <div ref={host} className="min-h-0 flex-1" />
    </div>
  )
}

/** A toolbar button, its name and keys in a tooltip. */
function Tool({
  label,
  keys,
  onClick,
  disabled,
  pressed,
  wide,
  children,
}: {
  label: string
  keys?: string
  onClick: () => void
  disabled?: boolean
  /** For a switch: whether it is on. */
  pressed?: boolean
  /** Text beside the icon rather than an icon alone. */
  wide?: boolean
  children: ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size={wide ? "sm" : "icon-sm"}
            aria-label={label}
            aria-pressed={pressed}
            disabled={disabled}
            onClick={onClick}
            className={cn(pressed && "bg-accent text-accent-foreground")}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>
        {label}
        {keys && <kbd className="font-sans opacity-70">{keys}</kbd>}
      </TooltipContent>
    </Tooltip>
  )
}

function withNewline(text: string): string {
  return text.endsWith("\n") ? text : `${text}\n`
}

/** Indents the document two spaces (formatSpec); nothing in a read-only editor. */
function formatCommand(view: EditorView): boolean {
  if (view.state.readOnly) return false
  const spec = formatSpec(view.state)
  if (spec) view.dispatch(spec)
  return spec !== null
}

/** Folds the parts inside the document, leaving its keys in view (foldContentsEffects). */
function foldContents(view: EditorView): boolean {
  const effects = foldContentsEffects(view.state)
  if (effects.length) view.dispatch({ effects })
  return effects.length > 0
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
  const hint: JsonKeyHint | undefined = pointer === null ? undefined : hintsAt(view.state.field(config).hints, pointer)?.find((h) => h.key === keyOf(node, doc))
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
  // Find (and replace): CodeMirror's panel, in the app's colours.
  ".cm-panels": { backgroundColor: "var(--muted)", color: "var(--foreground)" },
  ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--border)" },
  ".cm-panel.cm-search": { padding: "6px 8px", fontSize: "12px" },
  ".cm-panel.cm-search label": { color: "var(--muted-foreground)", fontSize: "12px" },
  ".cm-textfield": {
    backgroundColor: "var(--background)",
    color: "var(--foreground)",
    border: "1px solid var(--input)",
    borderRadius: "6px",
    padding: "2px 6px",
    fontSize: "12px",
  },
  ".cm-button": {
    backgroundImage: "none",
    backgroundColor: "var(--background)",
    color: "var(--foreground)",
    border: "1px solid var(--border)",
    borderRadius: "6px",
    padding: "2px 8px",
    fontSize: "12px",
  },
  ".cm-button:active": { backgroundImage: "none", backgroundColor: "var(--accent)" },
  ".cm-panel.cm-search [name=close]": { color: "var(--muted-foreground)", fontSize: "16px" },
  ".cm-searchMatch": { backgroundColor: "color-mix(in oklab, var(--json-literal) 22%, transparent)" },
  ".cm-searchMatch.cm-searchMatch-selected": {
    backgroundColor: "color-mix(in oklab, var(--json-key) 35%, transparent)",
  },
  ".cm-selectionMatch": { backgroundColor: "color-mix(in oklab, var(--json-key) 14%, transparent)" },
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
  search({ top: true }),
  keymap.of([
    { key: "Shift-Alt-f", run: formatCommand },
    // Before foldKeymap, whose Ctrl-Alt-[ would fold the whole document.
    { key: "Ctrl-Alt-[", run: foldContents },
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

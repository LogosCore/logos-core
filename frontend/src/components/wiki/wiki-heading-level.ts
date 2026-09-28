// The heading level badge: a small "H3" in the left margin of the heading the
// caret is in, which opens a menu to change the level (WikiHeadingLevelMenu).
//
// Headings are only slightly larger and bolder than body text, so a heading
// that has just been made with `###` + space and has no text yet was
// indistinguishable from an empty paragraph, and nothing in the UI changed a
// heading's level after the fact.
//
// A decoration, not a node view or an overlay: it sits at the start of the
// heading and is pushed into the margin with CSS, so it moves with the text
// without measuring anything on scroll or resize, and only one exists at a
// time. Decorations are local to this view — nothing reaches the document or
// collaborators — and the plugin draws nothing while the editor is read-only,
// so viewers and the print page never see it.

import { Extension, type Editor } from "@tiptap/core"
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state"
import { Decoration, DecorationSet } from "@tiptap/pm/view"

/** Fired on the badge (it bubbles to the editor DOM) when it is clicked. */
export const HEADING_LEVEL_OPEN_EVENT = "wiki-heading-level-open"

export interface HeadingLevelOpenDetail {
  /** Document position of the heading node, for the menu to act on. */
  pos: number
  level: number
}

export const headingLevelPluginKey = new PluginKey("wikiHeadingLevel")

/**
 * The heading the selection is in, if the whole selection is inside one.
 * A selection spanning several blocks gets no badge: which heading would a
 * level change apply to?
 */
export function currentHeading(state: EditorState): { pos: number; level: number } | null {
  const { $from, $to } = state.selection
  if (!$from.sameParent($to)) return null
  const node = $from.parent
  if (node.type.name !== "heading") return null
  return { pos: $from.before(), level: node.attrs.level as number }
}

function badge(pos: number, level: number): HTMLElement {
  // The wrapper keeps the heading's font so it can be exactly one heading line
  // tall (1lh) and centre the small label on the first line.
  const wrapper = document.createElement("span")
  wrapper.className = "wiki-heading-level"
  wrapper.contentEditable = "false"

  const button = document.createElement("button")
  button.type = "button"
  button.className = "wiki-heading-level__button"
  button.textContent = `H${level}`
  button.title = `Heading ${level} — change level`
  button.setAttribute("aria-label", `Heading ${level}, change level`)
  button.setAttribute("aria-haspopup", "menu")
  // Keep the caret where it is: a mousedown inside the editor would otherwise
  // move the selection, which would move (or remove) the badge being clicked.
  button.addEventListener("mousedown", (event) => event.preventDefault())
  button.addEventListener("click", (event) => {
    event.preventDefault()
    button.dispatchEvent(
      new CustomEvent<HeadingLevelOpenDetail>(HEADING_LEVEL_OPEN_EVENT, {
        bubbles: true,
        detail: { pos, level },
      }),
    )
  })

  wrapper.appendChild(button)
  return wrapper
}

export function headingLevelDecorations(state: EditorState, editable: boolean): DecorationSet {
  if (!editable) return DecorationSet.empty
  const heading = currentHeading(state)
  if (!heading) return DecorationSet.empty
  return DecorationSet.create(state.doc, [
    Decoration.widget(heading.pos + 1, () => badge(heading.pos, heading.level), {
      side: -1,
      // Keyed by position and level so a level change redraws the label, and
      // an unrelated transaction reuses the existing element.
      key: `heading-level-${heading.pos}-${heading.level}`,
      ignoreSelection: true,
      stopEvent: () => true,
    }),
  ])
}

export const WikiHeadingLevel = Extension.create({
  name: "wikiHeadingLevel",

  addProseMirrorPlugins() {
    const editor: Editor = this.editor
    return [
      new Plugin({
        key: headingLevelPluginKey,
        props: {
          decorations: (state) => headingLevelDecorations(state, editor.isEditable),
        },
      }),
    ]
  },
})

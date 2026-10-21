import type { AnyExtension } from "@tiptap/core"
import { ReactNodeViewRenderer } from "@tiptap/react"
import StarterKit from "@tiptap/starter-kit"
import TaskList from "@tiptap/extension-task-list"
import TaskItem from "@tiptap/extension-task-item"
import { TableRow, TableHeader, TableCell } from "@tiptap/extension-table"
import Image from "@tiptap/extension-image"
import { Link } from "@tiptap/extension-link"
import { HorizontalRule } from "@tiptap/extension-horizontal-rule"
import { CodeBlock } from "@tiptap/extension-code-block"
import { lowlight } from "@/lib/wiki-lowlight"
import { WikiTable } from "@/components/wiki/wiki-table"
import { WikiCodeBlock } from "@/components/wiki/wiki-code-block"
import { createIncrementalLowlightPlugin } from "@/components/wiki/wiki-code-block-highlight-plugin"
import { WikiHorizontalRuleNode } from "@/components/wiki/wiki-horizontal-rule-node"
import { WikiImageNode } from "@/components/wiki/wiki-image-node"
import { WikiFileExtension } from "@/components/wiki/wiki-file-node"
import { WikiCredentialReferenceExtension } from "@/components/wiki/wiki-credential-reference-node"
import { WikiHashReferenceExtension } from "@/components/wiki/wiki-hash-reference-node"
import { WikiHostReferenceExtension } from "@/components/wiki/wiki-host-reference-node"
import { WikiDocumentReferenceExtension } from "@/components/wiki/wiki-document-reference-node"
import { WikiHeadingLevel } from "@/components/wiki/wiki-heading-level"
import { WikiNoticeExtension } from "@/components/wiki/wiki-notice-node"
import { WikiChecklistItemExtension } from "@/components/wiki/wiki-checklist-item-node"
import { WikiHighlightMark } from "@/components/wiki/wiki-highlight-mark"

/**
 * The extensions that define what a wiki page *is*: its nodes, marks and how
 * each renders. Shared by the editor (wiki-editor.tsx), which adds the
 * editing machinery around them — collaboration, cursors, slash commands,
 * placeholders — and by the HTML bundle's offline viewer (src/viewer), which
 * renders the same JSON read-only. One list, so an exported page can never
 * render with a node the editor does not have, or miss one it does.
 *
 * A function, not a constant: Tiptap extensions are configured instances and
 * every editor needs its own.
 */
export function wikiContentExtensions(): AnyExtension[] {
  return [
    StarterKit.configure({
      undoRedo: false, // Y.js collaboration handles undo/redo
      codeBlock: false, // Replaced by CodeBlockLowlight below
      horizontalRule: false, // Replaced by custom NodeView below
      code: { HTMLAttributes: { class: "wiki-inline-code" } },
      // The bundled Link mark defaults to inclusive=true, which makes the
      // mark grow when the user types past its right boundary (e.g. typing
      // a space after an autolinked URL extends the link onto the space).
      // Disable here and re-register an extended Link below with
      // inclusive=false so the boundary is sticky.
      link: false,
    }),
    Link.extend({ inclusive: false }).configure({
      // Tiptap's "whenNotEditable" string actually resolves to true inside
      // the click plugin and still calls window.open in edit mode. Disable
      // outright; the read-only renderer relies on the browser default
      // (<a target=_blank>) for navigation, not Tiptap's click handler.
      openOnClick: false,
      // A click should just place the caret. The popover keys off
      // `editor.isActive('link')`, which is true for a collapsed selection
      // inside the mark, so we don't need to extend to the full mark range.
      enableClickSelection: false,
      autolink: true,
      linkOnPaste: true,
      defaultProtocol: "https",
      HTMLAttributes: {
        class: "wiki-link",
        rel: "noopener noreferrer nofollow",
        target: "_blank",
      },
    }),
    HorizontalRule.extend({
      addAttributes() {
        return {
          ...this.parent?.(),
          variant: {
            default: "line",
            parseHTML: (el) => {
              const v = el.getAttribute("data-variant")
              return v === "dashed" ? "dashed" : "line"
            },
            renderHTML: (attrs) =>
              attrs.variant && attrs.variant !== "line"
                ? { "data-variant": attrs.variant }
                : {},
          },
        }
      },
      addNodeView() {
        return ReactNodeViewRenderer(WikiHorizontalRuleNode)
      },
    }),
    // Use the base CodeBlock + our own incremental lowlight plugin instead
    // of @tiptap/extension-code-block-lowlight: upstream re-tokenizes every
    // code block on every keystroke (via highlight.js), which makes typing
    // in large blocks lag by 1-2 seconds per character. The incremental
    // variant memoizes per-block decorations and only re-runs highlight.js
    // for the block whose content actually changed. See
    // wiki-code-block-highlight-plugin.ts.
    CodeBlock.extend({
      addKeyboardShortcuts() {
        return {
          ...this.parent?.(),
          // Ctrl/Cmd+A inside a code block selects only that block's text
          // instead of the whole document. Falls through (returns false) to
          // ProseMirror's default select-all whenever the caret isn't in a
          // code block. The read-only equivalent lives in a native keydown
          // listener below — this keymap entry never fires when the editor
          // is not editable.
          "Mod-a": () => {
            const { $from } = this.editor.state.selection
            if ($from.parent.type.name !== this.name) return false
            return this.editor.commands.setTextSelection({
              from: $from.start(),
              to: $from.end(),
            })
          },
        }
      },
      addAttributes() {
        return {
          ...this.parent?.(),
          wrap: {
            default: false,
            parseHTML: (el) => el.classList.contains("is-wrapped"),
            renderHTML: (attrs) => (attrs.wrap ? { class: "is-wrapped" } : {}),
          },
          // Stable, write-once identity for a code block. Used solely as the
          // key for per-viewer collapse/expand state (see
          // wiki-code-expansion.ts) so that a viewer's expansion survives
          // y-prosemirror replacing node references on remote edits. Assigned
          // lazily by the NodeView on first edit-mode mount; never changed
          // after, so it syncs across collaborators with no conflict risk.
          blockId: {
            default: null,
            parseHTML: (el) => el.getAttribute("data-block-id"),
            renderHTML: (attrs) =>
              attrs.blockId ? { "data-block-id": attrs.blockId } : {},
          },
        }
      },
      addNodeView() {
        return ReactNodeViewRenderer(WikiCodeBlock, {
          // Custom update guard so a remote collaborator typing in some
          // unrelated paragraph doesn't re-render every code block in the
          // doc. Tiptap's default update returns true (triggering a React
          // render) whenever node/decorations/innerDecorations are a new
          // reference — true on every transaction, since both PM and our
          // lowlight plugin can produce fresh DecorationSet references
          // even when nothing observable changed for THIS block. The
          // WikiCodeBlock component only reads node.attrs.language,
          // node.attrs.wrap, and node.textContent (via marks recompute),
          // plus a cursorInside boolean from useEditorState — none of
          // which are derivable from the new decoration set. So when none
          // of those changed, skip the React render and let PM's view
          // layer reconcile the (no-op) decoration diff on its own.
          update: ({ oldNode, newNode, updateProps }) => {
            if (oldNode.type !== newNode.type) return false
            const renderInputsSame =
              oldNode.attrs.language === newNode.attrs.language &&
              oldNode.attrs.wrap === newNode.attrs.wrap &&
              oldNode.attrs.blockId === newNode.attrs.blockId &&
              oldNode.textContent === newNode.textContent
            if (!renderInputsSame) updateProps()
            return true
          },
        })
      },
      addProseMirrorPlugins() {
        return [
          ...(this.parent?.() ?? []),
          createIncrementalLowlightPlugin({
            name: this.name,
            lowlight,
            defaultLanguage: this.options.defaultLanguage,
          }),
        ]
      },
    }).configure({
      defaultLanguage: "plaintext",
      HTMLAttributes: { class: "wiki-code-block" },
    }),
    WikiHeadingLevel,
    TaskList,
    TaskItem.configure({ nested: true }),
    // WikiTable, not Table: the stock extension derives a column's default
    // width from cellMinWidth, which leaves a wide table narrower than the
    // content column so every column collapses to a letter. See wiki-table.ts.
    WikiTable.configure({
      resizable: true,
      allowTableNodeSelection: true,
      HTMLAttributes: { class: "wiki-table" },
    }),
    TableRow,
    TableHeader,
    TableCell,
    Image.extend({
      // Persist intrinsic dimensions captured at upload time. The base
      // Image extension only models src/alt/title, which means the <img>
      // ships with no `width`/`height` attributes and the layout shifts
      // on first decode — a problem that compounds across many images.
      // Storing the natural dimensions lets the browser reserve the
      // correct aspect ratio before pixels arrive (combined with the
      // CSS `max-width: 100%; height: auto`), eliminating per-image
      // decode-time reflows. Legacy nodes without these attrs render
      // exactly as before.
      addAttributes() {
        return {
          ...this.parent?.(),
          width: {
            default: null,
            parseHTML: (el) => {
              const raw = el.getAttribute("width")
              if (raw === null) return null
              const n = Number(raw)
              return Number.isFinite(n) && n > 0 ? n : null
            },
            renderHTML: (attrs) =>
              attrs.width ? { width: String(attrs.width) } : {},
          },
          height: {
            default: null,
            parseHTML: (el) => {
              const raw = el.getAttribute("height")
              if (raw === null) return null
              const n = Number(raw)
              return Number.isFinite(n) && n > 0 ? n : null
            },
            renderHTML: (attrs) =>
              attrs.height ? { height: String(attrs.height) } : {},
          },
        }
      },
      addNodeView() {
        return ReactNodeViewRenderer(WikiImageNode)
      },
    }).configure({
      // Images render as block-level nodes so they break the paragraph
      // flow rather than wedging inline — matches how paste/drop are used.
      inline: false,
      allowBase64: false,
      HTMLAttributes: { class: "wiki-image" },
    }),
    WikiFileExtension,
    WikiCredentialReferenceExtension,
    WikiHashReferenceExtension,
    WikiHostReferenceExtension,
    WikiDocumentReferenceExtension,
    WikiNoticeExtension,
    WikiChecklistItemExtension,
    WikiHighlightMark,
  ]
}

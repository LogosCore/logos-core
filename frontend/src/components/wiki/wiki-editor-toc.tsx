import { SquareCheckIcon, SquareIcon } from "lucide-react"
import { type Editor, useEditorState } from "@tiptap/react"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"

interface WikiEditorTocProps {
  editor: Editor
}

interface TocItem {
  /** "heading" entries carry their real heading level; "checklist" entries are
   *  indented one level under the most recent heading. */
  kind: "heading" | "checklist"
  level: number
  text: string
  pos: number
  /** Checklist items only: whether the question counts as covered, so the
   *  outline can mark answered ones. Always false for headings. */
  answered: boolean
}

// Whether a checklist item counts as covered, mirroring the item view's
// deriveStatus and the sidecar coverage projection: an explicit N/A counts as
// covered, a flagged item never does, otherwise it's covered iff the answer
// region holds content.
function isChecklistItemAnswered(node: {
  attrs: Record<string, unknown>
  textContent: string
  content: { childCount: number }
  firstChild: { content: { childCount: number } } | null
}): boolean {
  const state = (node.attrs.state as string) || ""
  if (state === "not_applicable") return true
  if (state === "flagged") return false
  return (
    node.textContent.trim().length > 0 ||
    node.content.childCount > 1 ||
    (node.firstChild?.content.childCount ?? 0) > 0
  )
}

// Indent class per heading level. Kept as a static lookup so Tailwind's JIT
// can statically detect every class; computing pl-{n} dynamically would
// generate strings the compiler never sees.
const INDENT_BY_LEVEL: Record<number, string> = {
  1: "pl-2",
  2: "pl-4",
  3: "pl-6",
  4: "pl-8",
  5: "pl-10",
  6: "pl-12",
}

/**
 * The page's outline, as items for the header's Outline menu.
 *
 * This used to be a panel floating over the editor's top-right corner, which
 * covered the text it was an index of and stayed up until dismissed. As menu
 * items it costs nothing while closed, and a click both navigates and closes
 * — the two things anyone ever wanted from it.
 *
 * Rendered only inside an open menu, so the useEditorState subscription below
 * exists only while someone is looking at the outline.
 */
export function WikiEditorToc({ editor }: WikiEditorTocProps) {
  // Walk the doc on every transaction and collect headings. The custom
  // equalityFn avoids re-rendering on unrelated edits (typing inside a
  // paragraph, cursor moves) by shallow-comparing the heading triples.
  const items = useEditorState({
    editor,
    selector: ({ editor: e }): TocItem[] => {
      const out: TocItem[] = []
      if (!e) return out
      // Track the most recent heading level so checklist items nest one level
      // beneath the section they fall under.
      let headingLevel = 0
      e.state.doc.descendants((node, pos) => {
        if (node.type.name === "heading") {
          headingLevel = Number(node.attrs.level ?? 1)
          out.push({
            kind: "heading",
            level: headingLevel,
            text: node.textContent.trim() || "Untitled",
            pos,
            answered: false,
          })
          return
        }
        if (node.type.name === "wikiChecklistItem") {
          out.push({
            kind: "checklist",
            level: Math.min(headingLevel + 1, 6),
            // The prompt lives in an attribute; textContent is the answer body.
            text: (node.attrs.prompt as string)?.trim() || "Untitled item",
            pos,
            answered: isChecklistItemAnswered(node),
          })
          // Don't descend into the answer region — its prose isn't outline-worthy.
          return false
        }
      })
      return out
    },
    equalityFn: (a, b) => {
      if (a === b) return true
      if (!a || !b || a.length !== b.length) return false
      for (let i = 0; i < a.length; i++) {
        const x = a[i]
        const y = b[i]
        if (
          x.pos !== y.pos ||
          x.level !== y.level ||
          x.text !== y.text ||
          x.kind !== y.kind ||
          x.answered !== y.answered
        ) {
          return false
        }
      }
      return true
    },
  }) ?? []

  function handleClick(pos: number) {
    // Resolve the heading's DOM node and scroll it into view. nodeDOM is
    // null when the node hasn't been rendered yet (e.g. mid-transaction);
    // fall back to coordsAtPos + scrollTo on the editor's scroll ancestor
    // in that case.
    const dom = editor.view.nodeDOM(pos)
    if (dom instanceof HTMLElement) {
      dom.scrollIntoView({ behavior: "smooth", block: "start" })
      return
    }
    const coords = editor.view.coordsAtPos(pos)
    const scrollParent = findScrollParent(editor.view.dom as HTMLElement)
    if (scrollParent && coords) {
      const containerRect = scrollParent.getBoundingClientRect()
      const top = scrollParent.scrollTop + (coords.top - containerRect.top)
      scrollParent.scrollTo({ top, behavior: "smooth" })
    }
  }

  if (items.length === 0) {
    return (
      <DropdownMenuItem disabled>Nothing to outline yet.</DropdownMenuItem>
    )
  }

  return (
    <>
      {items.map((item) => (
        <DropdownMenuItem
          key={item.pos}
          onClick={() => handleClick(item.pos)}
          className={cn(
            "text-xs",
            INDENT_BY_LEVEL[item.level] ?? "pl-2",
            item.kind === "heading" && item.level === 1 && "font-medium",
            item.kind === "heading" && item.level >= 3 && "text-muted-foreground",
            item.kind === "checklist" && "text-muted-foreground",
          )}
          title={item.text}
        >
          {item.kind === "checklist" &&
            (item.answered ? (
              <SquareCheckIcon className="size-3 shrink-0 text-emerald-500" />
            ) : (
              <SquareIcon className="size-3 shrink-0 opacity-60" />
            ))}
          <span className="truncate">{item.text}</span>
        </DropdownMenuItem>
      ))}
    </>
  )
}

// Walks up from the editor DOM until the nearest scrollable ancestor.
// The wiki editor lives inside `.flex-1.overflow-y-auto`, so the first
// ancestor with a scrolling overflow-y is the right target.
function findScrollParent(el: HTMLElement | null): HTMLElement | null {
  let node: HTMLElement | null = el
  while (node && node !== document.body) {
    const style = window.getComputedStyle(node)
    if (style.overflowY === "auto" || style.overflowY === "scroll") {
      return node
    }
    node = node.parentElement
  }
  return null
}

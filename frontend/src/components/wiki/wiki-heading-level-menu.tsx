import { useEffect, useState } from "react"
import type { Editor } from "@tiptap/core"
import { CheckIcon } from "lucide-react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu"
import {
  HEADING_LEVEL_OPEN_EVENT,
  type HeadingLevelOpenDetail,
} from "@/components/wiki/wiki-heading-level"

const LEVELS = [1, 2, 3, 4, 5, 6] as const

const IS_MAC =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)

/** Tiptap's own bindings: Mod-Alt-<level> for a heading, Mod-Alt-0 for text. */
function shortcut(digit: number): string {
  return IS_MAC ? `⌘⌥${digit}` : `Ctrl+Alt+${digit}`
}

// Positions the menu against whichever badge is in the DOM now. The badge is a
// decoration, and a transaction while the menu is open (a collaborator typing
// above) can redraw it as a new element, leaving a captured one detached.
function badgeAnchor(editor: Editor) {
  return {
    getBoundingClientRect: () =>
      editor.view.dom.querySelector(".wiki-heading-level__button")?.getBoundingClientRect() ??
      new DOMRect(),
  }
}

/**
 * The menu the heading level badge opens (see wiki-heading-level.ts): normal
 * text and H1–H6, with the keyboard shortcut for each, so the shortcuts that
 * already existed become discoverable.
 *
 * Acts on the selection rather than on the badge's position: opening the menu
 * never moved the caret out of the heading (the badge swallows its mousedown),
 * and toggling through the selection keeps undo and collaboration on the same
 * path as the shortcuts.
 */
export function WikiHeadingLevelMenu({ editor }: { editor: Editor | null }) {
  const [open, setOpen] = useState(false)
  const [level, setLevel] = useState<number | null>(null)

  useEffect(() => {
    if (!editor) return
    const dom = editor.view.dom
    const onOpen = (event: Event) => {
      const { level } = (event as CustomEvent<HeadingLevelOpenDetail>).detail
      setLevel(level)
      setOpen(true)
    }
    dom.addEventListener(HEADING_LEVEL_OPEN_EVENT, onOpen)
    return () => dom.removeEventListener(HEADING_LEVEL_OPEN_EVENT, onOpen)
  }, [editor])

  if (!editor) return null

  const choose = (next: number | null) => {
    const chain = editor.chain().focus()
    if (next === null) chain.setParagraph().run()
    else chain.setHeading({ level: next as 1 | 2 | 3 | 4 | 5 | 6 }).run()
  }

  return (
    <DropdownMenu
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        // Hand the caret back on Escape or an outside click, as a pick does.
        if (!next) editor.commands.focus()
      }}
    >
      {open && (
        <DropdownMenuContent anchor={badgeAnchor(editor)} className="w-44">
          <DropdownMenuItem onClick={() => choose(null)}>
            <span className="w-4" />
            <span className="flex-1">Text</span>
            <span className="text-xs text-muted-foreground">{shortcut(0)}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {LEVELS.map((l) => (
            <DropdownMenuItem key={l} onClick={() => choose(l)}>
              {l === level ? <CheckIcon className="size-4" /> : <span className="w-4" />}
              <span className="flex-1">Heading {l}</span>
              <span className="text-xs text-muted-foreground">{shortcut(l)}</span>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      )}
    </DropdownMenu>
  )
}

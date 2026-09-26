import { UsersIcon } from "lucide-react"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useWikiDocumentPresence } from "@/graphql/hooks/wiki"
import { useAuthStore } from "@/stores/auth"
import { avatarLabel } from "@/lib/avatar-label"
import { getCursorColor } from "@/lib/cursor-colors"

interface WikiPresenceMenuProps {
  documentId: string
}

/**
 * Who else is on this page.
 *
 * Renders nothing when you are alone, which is the usual case — a row of
 * avatars that is always just your own face is noise, and it told you
 * something you already knew. The presence query counts everyone connected,
 * self included, so the reader here is filtered out: the count on the button
 * and the names in the menu are both "other people", and one of them showing
 * up is the whole signal.
 *
 * The menu is informational — names and how they are coloured in the
 * document, nothing to click. Clicking a name would suggest jumping to their
 * cursor, which is not something we do.
 */
export function WikiPresenceMenu({ documentId }: WikiPresenceMenuProps) {
  const { data } = useWikiDocumentPresence(documentId)
  const me = useAuthStore((s) => s.user)

  const others = (data?.wikiDocumentPresence.activeEditors ?? []).filter(
    (editor) => editor.userId !== me?.userId,
  )
  if (others.length === 0) return null

  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  className="gap-1.5 px-2 text-muted-foreground"
                  aria-label={`${others.length} other ${
                    others.length === 1 ? "person" : "people"
                  } on this page`}
                />
              }
            >
              <UsersIcon className="size-4" />
              <span className="text-xs tabular-nums">{others.length}</span>
            </DropdownMenuTrigger>
          }
        />
        <TooltipContent>
          {others.length === 1
            ? "1 other person here"
            : `${others.length} other people here`}
        </TooltipContent>
      </Tooltip>

      <DropdownMenuContent align="end" className="w-auto min-w-52">
        {/* GroupLabel (DropdownMenuLabel) must sit inside a Group, or Base UI
            throws "MenuGroupContext is missing" and takes the page down with
            it. The rows belong in the same group, so the label names them. */}
        <DropdownMenuGroup>
          <DropdownMenuLabel>Also here</DropdownMenuLabel>
          {others.map((editor) => (
            // Plain rows rather than menu items: nothing here responds to a
            // click, so nothing should take focus or highlight on hover.
            <div
              key={editor.userId}
              className="flex items-center gap-2 px-1.5 py-1 text-sm"
            >
              <Avatar
                className="size-6 border-2"
                style={{ borderColor: getCursorColor(editor.userId) }}
              >
                <AvatarFallback className="text-[10px]">
                  {avatarLabel(editor.username)}
                </AvatarFallback>
              </Avatar>
              <span className="min-w-0 flex-1 truncate">{editor.username}</span>
            </div>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

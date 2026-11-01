import { useState } from "react";
import {
  SwordsIcon,
  LoaderIcon,
  TerminalSquareIcon,
  XIcon,
} from "lucide-react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { SidebarMenuButton } from "@/components/ui/sidebar";
import { useScopedOperationStore } from "@/stores/scoped-operation";
import { OperationTooltipBody } from "@/components/layout/operation-tooltip-body";
import { preloadable, useLoaded } from "@/lib/preloadable";

// The panel (search and the virtualized list) loads when the switcher is
// first hovered, focused or opened: the shell renders this button on every
// page, and the list's code belongs to the moment someone picks.
const panelCode = preloadable(() => import("@/components/layout/operation-switcher-panel"));
const warmPanel = () => void panelCode.load().catch(() => undefined);

export function OperationSwitcher() {
  const scopedOperation = useScopedOperationStore((s) => s.scopedOperation);
  const unscopeOperation = useScopedOperationStore((s) => s.unscopeOperation);
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        // Anchor for the first-login walkthrough's opening step. The tour finds
        // its targets by this attribute rather than by class or structure, so
        // restyling the sidebar cannot silently unhook the guide.
        data-tour="operation-switcher"
        onPointerEnter={warmPanel}
        onFocus={warmPanel}
        render={
          <SidebarMenuButton
            size="lg"
            // SidebarMenuButton hides its tooltip unless the sidebar is
            // collapsed, on the assumption that an expanded label is already
            // readable. That does not hold here: the name and description are
            // truncated at the sidebar's width whatever its state. The object
            // form spreads over the component's own props, so `hidden: false`
            // overrides just that rule for this one button and leaves the
            // shared default alone for every other caller.
            tooltip={{
              hidden: false,
              children: (
                <OperationTooltipBody
                  name={scopedOperation ? scopedOperation.name : "Logos"}
                  description={
                    scopedOperation
                      ? scopedOperation.description || "Active operation"
                      : "Command & Control"
                  }
                />
              ),
            }}
          />
        }
      >
        {scopedOperation ? (
          <>
            <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
              <SwordsIcon className="size-4" />
            </div>
            <div className="grid flex-1 text-left text-sm leading-tight">
              <span className="truncate font-medium">
                {scopedOperation.name}
              </span>
              <span className="truncate text-xs text-muted-foreground">
                {scopedOperation.description || "Active operation"}
              </span>
            </div>
            <div
              role="button"
              tabIndex={0}
              onClick={(e) => {
                e.stopPropagation();
                unscopeOperation();
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  e.stopPropagation();
                  unscopeOperation();
                }
              }}
              className="ml-auto flex size-6 items-center justify-center rounded-md text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors"
            >
              <XIcon className="size-3.5" />
            </div>
          </>
        ) : (
          <>
            <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
              <TerminalSquareIcon className="size-4" />
            </div>
            <div className="grid flex-1 text-left text-sm leading-tight">
              <span className="truncate font-medium">Logos</span>
              <span className="truncate text-xs text-muted-foreground">
                Command &amp; Control
              </span>
            </div>
          </>
        )}
      </PopoverTrigger>

      <PopoverContent
        // The walkthrough reads this element's existence as proof the operator
        // found the switcher, and spotlights it for the "pick one" step. The
        // popover rather than the list inside it: the panel opening is the
        // lesson, and the search box above the rows is part of what to show.
        data-tour="operation-list"
        // Sized independently of the trigger. Anchoring to the sidebar's width
        // (16rem, less padding) left almost every name and description cut off,
        // and the collapsed sidebar would have pinned it narrower still. Capped
        // against the viewport so a narrow window can't push it off-screen.
        className="w-[min(26rem,calc(100vw-2rem))] p-1.5 overflow-hidden"
        side="bottom"
        align="start"
        sideOffset={4}
      >
        <SwitcherPanel onClose={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  );
}

/** The panel once its code is here; a spinner the first time it loads. */
function SwitcherPanel({ onClose }: { onClose: () => void }) {
  const code = useLoaded(panelCode);
  if (!code) {
    return (
      <div className="flex items-center justify-center gap-2 py-4 text-sm text-muted-foreground">
        <LoaderIcon className="size-3.5 animate-spin" />
      </div>
    );
  }
  return <code.default onClose={onClose} />;
}

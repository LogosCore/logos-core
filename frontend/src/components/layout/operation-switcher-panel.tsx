import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useConnectionNodes } from "@/hooks/use-connection-nodes";
import { useRecentOperations } from "@/hooks/use-picker-history";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import { CheckIcon, SwordsIcon, LoaderIcon, SearchIcon } from "lucide-react";
import { Input } from "@/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useInfiniteOperations } from "@/graphql/hooks/operations";
import { useScopedOperationStore } from "@/stores/scoped-operation";
import { buildPickerRows } from "@/components/layout/operation-picker-rows";
import { OperationTooltipBody } from "@/components/layout/operation-tooltip-body";

// The operation switcher's open panel: a search box over the operations,
// recent ones first, a page at a time as the list scrolls. Its own module,
// loaded when the switcher is first opened (or hovered): the virtualized list
// it needs is a page's weight, not the app shell's, and the shell renders
// the switcher's button on every page. It mounts with the popover each time
// it opens, so every opening starts with an empty search.

export default function OperationSwitcherPanel({
  onClose,
}: {
  /** Closes the popover: an operation picked, or Escape. */
  onClose: () => void;
}) {
  const scopedOperation = useScopedOperationStore((s) => s.scopedOperation);
  const recentOperations = useRecentOperations();
  const scopeOperation = useScopedOperationStore((s) => s.scopeOperation);

  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  // Tracks what moved the highlight. Only keyboard navigation should
  // programmatically scroll; mouse hover must not (the item is already under
  // the cursor, and scrolling would yank the viewport out from under it,
  // producing a self-scrolling loop as new items slide under the pointer).
  const scrollOnHighlight = useRef(false);

  // Callback ref — focuses the search input when the popover mounts it.
  const inputCallbackRef = useCallback((node: HTMLInputElement | null) => {
    if (node) node.focus();
  }, []);

  // Debounce search — fires query 300ms after the user stops typing.
  useEffect(() => {
    const timeout = setTimeout(() => setDebouncedSearch(search), 300);
    return () => clearTimeout(timeout);
  }, [search]);

  // Fetched only while the panel is mounted, which is while the popover is
  // open: the switcher's button renders the scoped operation from
  // localStorage/Zustand, so no page load pays for the list.
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } =
    useInfiniteOperations({
      search: debouncedSearch || null,
      first: 20,
    });

  const loadedOperations = useConnectionNodes(data, (p) => p.operations);
  const rows = useMemo(
    () =>
      buildPickerRows(recentOperations, loadedOperations, !!debouncedSearch),
    [recentOperations, loadedOperations, debouncedSearch],
  );
  const operations = useMemo(() => rows.map((r) => r.op), [rows]);

  // Reset the highlight when the operations list reference changes. Done
  // during render via the prev-value pattern
  // (react.dev/.../storing-information-from-previous-renders) rather than
  // setState-in-effect.
  const [lastOperations, setLastOperations] = useState(operations);
  if (lastOperations !== operations) {
    setLastOperations(operations);
    setHighlightedIndex(-1);
  }

  // Scroll highlighted item into view via Virtuoso — keyboard navigation only.
  useEffect(() => {
    if (highlightedIndex >= 0 && scrollOnHighlight.current) {
      virtuosoRef.current?.scrollIntoView({
        index: highlightedIndex,
        behavior: "auto",
      });
    }
    scrollOnHighlight.current = false;
  }, [highlightedIndex]);

  function selectOperation(op: (typeof operations)[number]) {
    scopeOperation({
      id: op.id,
      name: op.name,
      description: op.description ?? "",
    });
    onClose();
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      scrollOnHighlight.current = true;
      setHighlightedIndex((prev) =>
        prev < operations.length - 1 ? prev + 1 : 0,
      );
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      scrollOnHighlight.current = true;
      setHighlightedIndex((prev) =>
        prev > 0 ? prev - 1 : operations.length - 1,
      );
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (highlightedIndex >= 0 && highlightedIndex < operations.length) {
        selectOperation(operations[highlightedIndex]);
      }
    }
  }

  return (
    <>
      {/* Search input */}
      <div className="relative">
        <SearchIcon className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          ref={inputCallbackRef}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Search operations..."
          className="h-7 pl-7 text-xs"
        />
      </div>

      {/* Operations list */}
      {isLoading && (
        <div className="flex items-center justify-center gap-2 py-4 text-sm text-muted-foreground">
          <LoaderIcon className="size-3.5 animate-spin" />
        </div>
      )}

      {!isLoading && operations.length === 0 && (
        <div className="py-4 text-center text-sm text-muted-foreground">
          No operations found
        </div>
      )}

      {!isLoading && operations.length > 0 && (
        <Virtuoso
          ref={virtuosoRef}
          data={rows}
          style={{ height: "256px" }}
          endReached={() => {
            if (hasNextPage && !isFetchingNextPage) fetchNextPage();
          }}
          overscan={100}
          role="listbox"
          itemContent={(index, row) => {
            const op = row.op;
            const isActive = scopedOperation?.id === op.id;

            return (
              // Widening the popover shortens the truncation but cannot end
              // it — operation descriptions are free text. The tooltip is
              // what guarantees the full value is always reachable.
              // Only the rows Virtuoso has mounted carry one, so the cost
              // tracks the viewport rather than the size of the result set.
              <div key={op.id}>
                {row.section && (
                  <div className="px-1.5 pb-0.5 pt-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                    {row.section}
                  </div>
                )}
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        role="option"
                        aria-selected={index === highlightedIndex}
                        className={cn(
                          "flex w-full items-center gap-2 rounded-md px-1.5 py-1.5 text-left text-sm",
                          index === highlightedIndex
                            ? "bg-accent text-accent-foreground"
                            : "hover:bg-accent/50",
                        )}
                        onMouseEnter={() => {
                          scrollOnHighlight.current = false;
                          setHighlightedIndex(index);
                        }}
                        onMouseDown={(e) => {
                          e.preventDefault();
                          selectOperation(op);
                        }}
                      />
                    }
                  >
                    <SwordsIcon className="size-5 shrink-0 text-muted-foreground" />
                    <div className="grid flex-1 min-w-0 leading-tight">
                      <span className="truncate text-sm font-medium">
                        {op.name}
                      </span>
                      {op.description && (
                        <span className="truncate text-xs text-muted-foreground">
                          {op.description}
                        </span>
                      )}
                    </div>
                    {isActive && (
                      <CheckIcon className="size-3.5 shrink-0 text-primary" />
                    )}
                  </TooltipTrigger>
                  {/* To the right of the popover, so it never covers the rows
                    above and below the one being read. */}
                  <TooltipContent side="right" align="center">
                    <OperationTooltipBody
                      name={op.name}
                      description={op.description}
                    />
                  </TooltipContent>
                </Tooltip>
              </div>
            );
          }}
          components={{
            Footer: () => {
              if (isFetchingNextPage) {
                return (
                  <div className="flex items-center justify-center py-2">
                    <LoaderIcon className="size-3.5 animate-spin" />
                  </div>
                );
              }
              return null;
            },
          }}
        />
      )}
    </>
  );
}

import { useState } from "react"
import { CheckCheckIcon, LoaderIcon, MessageSquareWarningIcon } from "lucide-react"
import { toast } from "sonner"
import { Virtuoso } from "react-virtuoso"
import { Button } from "@/components/ui/button"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useCanBuild } from "@/components/builds/build-access"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useMarkMinionFeedbackRead, useMinionFeedback, type FeedbackFilter } from "@/graphql/hooks/minion-sessions"
import { FeedbackCounts, FeedbackEntry } from "./feedback-parts"
import { toggled } from "./feedback-values"

// A session's feedback (its Feedback tab): the warnings and errors its
// minion sent, newest first, paged and searched by core; unread ones marked
// until someone marks them read, for everyone in the operation. A long one
// shows its first line until opened.

export function FeedbackView({ s }: { s: MinionSessionFieldsFragment }) {
  const [filter, setFilter] = useState<FeedbackFilter>({ level: "", search: "" })
  // The entries shown whole: kept here, the list's rows come and go.
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  const q = useMinionFeedback(s.id, filter, s.feedbackAt, s.feedbackReadAt)
  const entries = (q.data?.pages ?? []).flatMap((p) => p.minionFeedback.entries)
  const markRead = useMarkMinionFeedbackRead()
  const access = useCanBuild(s.operationId)
  const unread = s.feedbackErrors + s.feedbackWarnings
  const id = s.id
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2">
        <Select value={filter.level} onValueChange={(v) => setFilter((f) => ({ ...f, level: (v ?? "") as FeedbackFilter["level"] }))}>
          <SelectTrigger size="sm" className="w-32" aria-label="Level">
            <SelectValue>{(v: string) => (v === "error" ? "Errors" : v === "warning" ? "Warnings" : "All levels")}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">All levels</SelectItem>
            <SelectItem value="error">Errors</SelectItem>
            <SelectItem value="warning">Warnings</SelectItem>
          </SelectContent>
        </Select>
        <SearchInput
          value={filter.search}
          onValueChange={(search) => setFilter((f) => ({ ...f, search }))}
          placeholder="Search messages and codes"
          className="relative w-64"
        />
        <span className="ml-auto flex items-center gap-3 text-sm text-muted-foreground">
          {unread > 0 ? <FeedbackCounts errors={s.feedbackErrors} warnings={s.feedbackWarnings} /> : "All read"}
          <Button
            variant="outline"
            size="sm"
            disabled={unread === 0 || !access.canWrite || markRead.isPending}
            title={access.canWrite ? "Marks every entry read, for everyone in the operation" : "Viewers read feedback; operators mark it read"}
            onClick={() =>
              markRead.mutate(id, { onError: (err) => toast.error(errorMessage(err, "Could not mark it read")) })
            }
          >
            <CheckCheckIcon />
            Mark all read
          </Button>
        </span>
      </div>
      {q.isLoading ? (
        <div className="flex flex-1 items-center justify-center p-6 text-muted-foreground">
          <LoaderIcon className="size-4 animate-spin" />
        </div>
      ) : q.isError ? (
        <p className="p-6 text-sm text-red-600 dark:text-red-400">{q.error instanceof Error ? q.error.message : "Couldn't read the feedback."}</p>
      ) : entries.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
          <MessageSquareWarningIcon className="size-6 opacity-50" />
          <p className="text-sm">
            {filter.level || filter.search.trim() ? "No feedback matches." : "The minion has sent no warnings or errors."}
          </p>
        </div>
      ) : (
        <Virtuoso
          data={entries}
          className="min-h-0 flex-1"
          style={{ height: "100%" }}
          computeItemKey={(_, e) => e.id}
          endReached={() => q.hasNextPage && !q.isFetchingNextPage && void q.fetchNextPage()}
          context={open}
          itemContent={(_, e, shown) => (
            <FeedbackEntry entry={e} showTask open={shown.has(e.id)} onToggle={() => setOpen((o) => toggled(o, e.id))} />
          )}
          components={{
            Footer: () =>
              q.isFetchingNextPage ? (
                <div className="flex justify-center py-2">
                  <LoaderIcon className="size-4 animate-spin text-muted-foreground" />
                </div>
              ) : null,
          }}
        />
      )}
    </div>
  )
}

import { FingerprintIcon, LoaderIcon } from "lucide-react"
import { Virtuoso } from "react-virtuoso"
import { Badge } from "@/components/ui/badge"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { CredentialValueNames } from "@/components/findings/credential-field-control"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useMinionIdentities } from "@/graphql/hooks/minion-sessions"
import { ELEVATED, howLabel, identityUser, scopeLabel } from "./identity-values"

// A session's Identity tab: who its process is, who its tasks run as now
// (a become, a stolen token), and every change, newest first, paged by core.

export function IdentityView({ s }: { s: MinionSessionFieldsFragment }) {
  const q = useMinionIdentities(s.id, s.rev)
  const entries = (q.data?.pages ?? []).flatMap((p) => p.minionIdentities.entries)
  const process = identityUser(s)
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="grid gap-x-6 gap-y-1 border-b px-4 py-3 text-sm sm:grid-cols-[auto_1fr]">
        <span className="text-muted-foreground">Process</span>
        <span>
          {process || <span className="text-muted-foreground">unknown</span>}
          {s.integrity && <span className="text-muted-foreground"> · {s.integrity}</span>}
        </span>
        <span className="text-muted-foreground">Tasks run as</span>
        <span>
          {s.acting ? (
            <span className="inline-flex flex-wrap items-baseline gap-x-2">
              <span className="font-medium">{identityUser(s.acting)}</span>
              {s.acting.integrity && (
                <span className={ELEVATED.has(s.acting.integrity) ? "font-medium" : "text-muted-foreground"}>· {s.acting.integrity}</span>
              )}
              <span className="text-muted-foreground">
                {howLabel(s.acting.how)} <FormattedDateTimeText date={s.acting.since} />
              </span>
              {s.acting.credentialId && <CredentialValueNames value={s.acting.credentialId} />}
            </span>
          ) : (
            <span className="text-muted-foreground">the process's own</span>
          )}
        </span>
      </div>
      {q.isLoading ? (
        <div className="flex flex-1 items-center justify-center p-6 text-muted-foreground">
          <LoaderIcon className="size-4 animate-spin" />
        </div>
      ) : q.isError ? (
        <p className="p-6 text-sm text-red-600 dark:text-red-400">{q.error instanceof Error ? q.error.message : "Couldn't read the history."}</p>
      ) : entries.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center text-muted-foreground">
          <FingerprintIcon className="size-6 opacity-50" />
          <p className="text-sm">No identity changes.</p>
        </div>
      ) : (
        <Virtuoso
          data={entries}
          className="min-h-0 flex-1"
          style={{ height: "100%" }}
          computeItemKey={(_, e) => e.id}
          endReached={() => q.hasNextPage && !q.isFetchingNextPage && void q.fetchNextPage()}
          itemContent={(_, e) => (
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-b px-4 py-1.5 text-sm">
              <span className="w-36 shrink-0 font-mono text-xs text-muted-foreground tabular-nums">
                <FormattedDateTimeText date={e.at} />
              </span>
              <Badge variant="outline">{scopeLabel(e.scope)}</Badge>
              <span className="font-medium">{identityUser(e) || "unknown"}</span>
              {e.integrity && <span className="text-muted-foreground">· {e.integrity}</span>}
              <span className="text-muted-foreground">
                {e.how ? howLabel(e.how) : e.scope === "process" ? "the process" : "back to the process's own"}
              </span>
              {e.credentialId && <CredentialValueNames value={e.credentialId} />}
              {e.taskId && (
                <span className="ms-auto font-mono text-xs text-muted-foreground" title={`Task ${e.taskId}`}>
                  task {e.taskId.slice(0, 8)}
                </span>
              )}
            </div>
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

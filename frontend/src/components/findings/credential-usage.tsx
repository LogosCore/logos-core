import { TriangleAlertIcon } from "lucide-react"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { useCredentialUsage } from "@/graphql/hooks/credentials"
import { liveUses } from "@/components/findings/credential-usage-text"

/** A warning line, as the dialogs show one. */
export function UsageWarning({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 rounded-md bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400">
      <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
      <span>{children}</span>
    </div>
  )
}

/**
 * Where the credential is used: the targets (with how it last fared logging
 * in to each) and endpoints whose values name it, and the builds and tasks it
 * went into. Hidden when nothing names it.
 */
export function CredentialUsageSection({ credentialId }: { credentialId: string }) {
  const { data: u, isLoading } = useCredentialUsage(credentialId)
  if (isLoading || !u || (liveUses(u) === 0 && u.builds === 0 && u.tasks === 0)) return null
  const history = [
    u.builds > 0 ? `went into ${u.builds} build${u.builds === 1 ? "" : "s"}` : "",
    u.tasks > 0 ? `carried by ${u.tasks} task${u.tasks === 1 ? "" : "s"}` : "",
  ].filter(Boolean)
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Used by</h3>
      {u.targets.map((t) => (
        <div key={t.id} className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 rounded-md border bg-muted/30 px-2 py-1 text-sm">
          <span className="font-medium">{t.name}</span>
          <span className="text-xs text-muted-foreground">
            {t.moduleName} target · {t.kind}
          </span>
          <span className="ms-auto text-xs">
            {!t.login ? (
              <span className="text-muted-foreground">not tried yet</span>
            ) : t.login.accepted ? (
              <span className="text-emerald-600 dark:text-emerald-400">
                logged in <FormattedDateTimeText date={t.login.at} />
              </span>
            ) : (
              <span className="text-destructive">
                refused <FormattedDateTimeText date={t.login.at} />
              </span>
            )}
          </span>
        </div>
      ))}
      {u.endpoints.map((e) => (
        <div key={e.id} className="flex flex-wrap items-baseline gap-x-2 rounded-md border bg-muted/30 px-2 py-1 text-sm">
          <span className="font-medium">{e.name}</span>
          <span className="text-xs text-muted-foreground">
            {e.moduleName} endpoint · {e.kind}
          </span>
        </div>
      ))}
      {history.length > 0 && (
        <p className="text-xs text-muted-foreground">It {history.join(" and ")}: those keep it as it was.</p>
      )}
    </section>
  )
}

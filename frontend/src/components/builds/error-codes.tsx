import { IdRows, InfoCard } from "@/components/ui/info-card"
import type { ErrorCode } from "@/components/builds/build-values"

/**
 * A factory's error codes (read-only), by code: what a minion's exit code
 * means when it stops on an error, each code with its label and
 * description. On the factory's page, and on a build's Overview, where an
 * operator looks up the code a minion of it exited with.
 */
export function ErrorCodes({ codes, labelWidth }: { codes: readonly ErrorCode[]; labelWidth?: string }) {
  const rows = [...codes]
    .sort((a, b) => a.code - b.code)
    .map((c) => ({
      id: String(c.code),
      content: (
        <>
          <div>{c.label}</div>
          {c.description && <div className="text-xs text-muted-foreground">{c.description}</div>}
        </>
      ),
    }))
  return (
    <InfoCard title="Minion error codes" note={codes.length} labelWidth={labelWidth}>
      <IdRows rows={rows} />
    </InfoCard>
  )
}

import { IdRows, InfoCard, NoteRow } from "@/components/ui/info-card"
import type { ErrorCode } from "@/components/builds/build-values"

/**
 * Error codes (read-only), by the code a minion exits with: what it means
 * when it stops on an error, each with its label and description. On the
 * factory's page, its declared codes (perBuild: each build draws its own,
 * and these are the defaults); on a build's Overview, the build's own
 * (drawn: core drew them for it), where an operator looks up the code a
 * minion of it exited with. A fixed code is the same in every build.
 */
export function ErrorCodes({
  codes,
  labelWidth,
  perBuild,
  drawn,
}: {
  codes: readonly ErrorCode[]
  labelWidth?: string
  perBuild?: boolean
  drawn?: boolean
}) {
  const exit = (c: ErrorCode) => c.exitCode ?? c.code
  const rows = [...codes]
    .sort((a, b) => exit(a) - exit(b))
    .map((c) => ({
      id: String(exit(c)),
      content: (
        <>
          <div>
            {c.label}
            {c.fixed && (perBuild || drawn) && <span className="text-muted-foreground"> · fixed</span>}
          </div>
          {c.description && <div className="text-xs text-muted-foreground">{c.description}</div>}
        </>
      ),
    }))
  return (
    <InfoCard title="Minion error codes" note={codes.length} labelWidth={labelWidth}>
      <IdRows rows={rows} />
      {perBuild && (
        <NoteRow>
          Each build gets its own exit codes, drawn from 3 to 255 for all but the fixed ones: its Overview lists them.
          These are the defaults, for a build that turns that off.
        </NoteRow>
      )}
      {drawn && <NoteRow>Drawn for this build: its minion exits with these, not its factory&apos;s defaults.</NoteRow>}
    </InfoCard>
  )
}

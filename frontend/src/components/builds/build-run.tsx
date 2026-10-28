import { useLayoutEffect, useRef } from "react"
import { CopyIcon, DownloadIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { downloadBlob } from "@/lib/file-export"
import { useNow } from "@/hooks/use-now"
import { isUnfinished } from "@/graphql/hooks/builds"
import { formatElapsed } from "@/components/modules/management/field-values"
import { BuildPipeline } from "@/components/builds/build-pipeline"
import { BuildStateBadge } from "@/components/builds/build-state-badge"
import { buildElapsed } from "@/components/builds/build-values"
import { BuildNotices, Section } from "@/components/builds/build-detail-parts"
import type { BuildRecord } from "@/components/builds/build-record"

/**
 * The build modal's Pipeline tab: how the build went. Its stages on the
 * dotted canvas, its state and time, the error and the factory's warnings,
 * and its log under them, filling what is left. It changes as the build
 * runs (the build query polls while it does).
 */
export function BuildRun({ build: b }: { build: BuildRecord }) {
  const now = useNow()
  const running = isUnfinished(b.state)
  const elapsed = b.startedAt || !running ? formatElapsed(buildElapsed(b, now)) : null
  return (
    <div className="flex min-h-full min-w-0 flex-col gap-6 px-4 py-6">
      <Section
        title="Pipeline"
        className="dot-grid"
        aside={
          <>
            <BuildStateBadge build={b} />
            {elapsed && (
              <span className="text-xs text-muted-foreground tabular-nums">
                {running ? `${elapsed} so far` : `took ${elapsed}`}
              </span>
            )}
          </>
        }
      >
        <div className="py-3">
          <BuildPipeline stages={b.stages} now={now} />
        </div>
      </Section>

      <BuildNotices build={b} />

      <BuildLog log={b.log ?? ""} running={running} fileName={`${b.id}.log`} />
    </div>
  )
}

/**
 * The build's output as the factory wrote it (core keeps its last 64 KiB).
 * While the build runs it stays at the end as lines come in, until the
 * reader scrolls up to read; scrolling back to the end follows again.
 */
function BuildLog({ log, running, fileName }: { log: string; running: boolean; fileName: string }) {
  const box = useRef<HTMLPreElement>(null)
  const follow = useRef(true)

  useLayoutEffect(() => {
    const el = box.current
    if (el && follow.current) el.scrollTop = el.scrollHeight
  }, [log])

  return (
    <section className="flex min-h-72 flex-1 flex-col">
      <div className="mb-1 flex min-h-5 items-center gap-2">
        <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Build log</h2>
        <span className="text-xs text-muted-foreground">the factory&apos;s output, its last 64 KiB</span>
        <span className="flex-1" />
        <Button
          variant="ghost"
          size="icon-xs"
          title="Copy the log"
          aria-label="Copy the log"
          disabled={!log}
          onClick={() => void copyToClipboard(log, "log")}
        >
          <CopyIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title={`Download as ${fileName}`}
          aria-label="Download the log"
          disabled={!log}
          onClick={() => downloadBlob(log, fileName, "text/plain")}
        >
          <DownloadIcon />
        </Button>
      </div>
      {log ? (
        <pre
          ref={box}
          onScroll={(e) => {
            const el = e.currentTarget
            follow.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
          }}
          className="min-h-0 flex-1 overflow-auto rounded-xl border bg-muted/40 p-3 font-mono text-xs leading-5 break-words whitespace-pre-wrap"
        >
          {log}
        </pre>
      ) : (
        <div className="flex flex-1 items-center justify-center rounded-xl border text-sm text-muted-foreground">
          {running ? "No output yet." : "The factory sent no output."}
        </div>
      )}
    </section>
  )
}

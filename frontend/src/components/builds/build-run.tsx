import { useLayoutEffect, useMemo, useRef, useState } from "react"
import { ChevronRightIcon, ChevronsDownUpIcon, ChevronsUpDownIcon, CopyIcon, DownloadIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { downloadBlob } from "@/lib/file-export"
import { useNow } from "@/hooks/use-now"
import { cn } from "@/lib/utils"
import { formatTime } from "@/lib/date-format"
import { isUnfinished, useBuildLog } from "@/graphql/hooks/builds"
import { BuildPipeline, StageIcon } from "@/components/builds/build-pipeline"
import { logSections, logText, missingText, openByDefault, type LogSection } from "@/components/builds/build-log"
import { BuildNotices, Section } from "@/components/builds/build-detail-parts"
import type { BuildRecord } from "@/components/builds/build-record"

/**
 * The build modal's Pipeline tab: how the build went. Its stages on the
 * dotted canvas (the build's state and time are in the modal's header), the
 * error and the factory's warnings, and its log by step under them, filling
 * what is left. It changes as the build runs (the build query and the log
 * poll while it does).
 */
export function BuildRun({ build: b }: { build: BuildRecord }) {
  const running = isUnfinished(b.state)
  return (
    <div className="flex min-h-full min-w-0 flex-col gap-6 px-4 py-6">
      <Section title="Pipeline" className="dot-grid">
        <div className="py-3">
          <LiveBuildPipeline stages={b.stages} />
        </div>
      </Section>

      <BuildNotices build={b} />

      <BuildLog build={b} running={running} />
    </div>
  )
}

/** The pipeline, timing a running stage on the shared clock: a tick re-renders it, not the tab. */
function LiveBuildPipeline({ stages }: { stages: BuildRecord["stages"] }) {
  const now = useNow()
  return <BuildPipeline stages={stages} now={now} />
}

/**
 * The build's log by step, as the factory wrote it: a section for each step
 * that wrote lines, in the plan's order, with its state and line count; the
 * running or failed step open, the others a click away. While the build runs
 * it stays at the end as lines come in, until the reader scrolls up to read;
 * scrolling back to the end follows again. A factory that sends its log with
 * its result gives one section of it, without steps or times.
 */
function BuildLog({ build: b, running }: { build: BuildRecord; running: boolean }) {
  const log = useBuildLog(b.id, running)
  const sections = useMemo(() => logSections(log.lines, b.stages), [log.lines, b.stages])
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const isOpen = (s: LogSection) => open[s.id] ?? openByDefault(s, sections)
  const allOpen = sections.every(isOpen)
  const box = useRef<HTMLDivElement>(null)
  const follow = useRef(true)
  const lines = log.lines.length

  useLayoutEffect(() => {
    const el = box.current
    if (el && follow.current) el.scrollTop = el.scrollHeight
  }, [lines, open])

  const text = () => logText(log, sections)
  const fileName = `${b.id}.log`
  return (
    <section className="flex min-h-72 flex-1 flex-col">
      <div className="mb-1 flex min-h-5 items-center gap-2">
        <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Build log</h2>
        <span className="truncate text-xs text-muted-foreground">
          {log.live || lines === 0 ? "the factory's output, by step" : "the factory's output with its result, its last 64 KiB"}
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          className="ml-auto"
          title={allOpen ? "Collapse all steps" : "Expand all steps"}
          aria-label={allOpen ? "Collapse all steps" : "Expand all steps"}
          disabled={sections.length < 2}
          onClick={() => setOpen(Object.fromEntries(sections.map((s) => [s.id, !allOpen])))}
        >
          {allOpen ? <ChevronsDownUpIcon /> : <ChevronsUpDownIcon />}
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title="Copy the log"
          aria-label="Copy the log"
          disabled={lines === 0}
          onClick={() => void copyToClipboard(text(), "log")}
        >
          <CopyIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          title={`Download as ${fileName}`}
          aria-label="Download the log"
          disabled={lines === 0}
          onClick={() => downloadBlob(text(), fileName, "text/plain")}
        >
          <DownloadIcon />
        </Button>
      </div>
      {lines > 0 ? (
        <div
          ref={box}
          onScroll={(e) => {
            const el = e.currentTarget
            follow.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
          }}
          className="min-h-0 flex-1 overflow-auto rounded-xl border bg-muted/40 py-1 font-mono text-xs leading-5"
        >
          {log.trimmed > 0 && (
            <p className="px-3 py-1 text-muted-foreground italic">
              … {log.trimmed} earlier lines dropped: core keeps a build&apos;s last 8 MiB
            </p>
          )}
          {sections.map((s) => (
            <LogSectionView
              key={s.id}
              section={s}
              header={log.live}
              open={isOpen(s)}
              onToggle={() => setOpen((o) => ({ ...o, [s.id]: !isOpen(s) }))}
            />
          ))}
        </div>
      ) : (
        <div className="flex flex-1 items-center justify-center rounded-xl border text-sm text-muted-foreground">
          {running ? "No output yet." : "The factory sent no output."}
        </div>
      )}
    </section>
  )
}

/** One step's lines under a header that opens and closes them; a result's log has no header. */
function LogSectionView({
  section: s,
  header,
  open,
  onToggle,
}: {
  section: LogSection
  header: boolean
  open: boolean
  onToggle: () => void
}) {
  return (
    <div>
      {header && (
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex w-full min-w-0 items-center gap-2 px-2 py-1 text-left font-sans text-sm hover:bg-muted"
        >
          <ChevronRightIcon className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
          {s.state ? <StageIcon state={s.state} /> : <span className="size-4 shrink-0" />}
          <span className="truncate">{s.label}</span>
          <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">
            {s.count} {s.count === 1 ? "line" : "lines"}
          </span>
        </button>
      )}
      {(open || !header) && (
        <div className={cn("pb-1", header && "pl-6")}>
          {s.lines.map((l, i) =>
            l.missing != null ? (
              <div key={i} className="px-3 text-muted-foreground italic">
                {missingText(l.missing)}
              </div>
            ) : (
              <div key={i} className="flex gap-3 px-3">
                {l.at && <span className="shrink-0 text-muted-foreground/70 tabular-nums select-none">{formatTime(l.at)}</span>}
                <span className="min-w-0 break-words whitespace-pre-wrap">{l.text}</span>
              </div>
            ),
          )}
        </div>
      )}
    </div>
  )
}

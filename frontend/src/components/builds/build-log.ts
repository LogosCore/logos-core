import { formatTime } from "@/lib/date-format"

/** A line of a build's log, or a mark (missing set) where lines are missing. */
export interface LogLine {
  step?: string | null
  at?: string | null
  text: string
  missing?: number | null
}

/** A page of a build's log, as the buildLog query returns it. */
export interface LogPage {
  live: boolean
  cursor: number
  more: boolean
  trimmed: number
  lines: readonly LogLine[]
}

/** A build's log as read so far. */
export interface BuildLogState {
  /** The factory sent it as it built, by step; otherwise it is its result's log. */
  live: boolean
  lines: readonly LogLine[]
  /** Where the next page starts. */
  cursor: number
  /** Lines core dropped from the log's start. */
  trimmed: number
}

export const EMPTY_LOG: BuildLogState = { live: false, lines: [], cursor: 0, trimmed: 0 }

/**
 * The log with a page read after it: a live log's lines come after those
 * read; a result's log comes whole each time, and replaces them.
 */
export function mergeLogPage(log: BuildLogState, page: LogPage): BuildLogState {
  if (!page.live) return { live: false, lines: page.lines, cursor: page.cursor, trimmed: 0 }
  return {
    live: true,
    lines: log.live ? [...log.lines, ...page.lines] : page.lines,
    cursor: page.cursor,
    trimmed: page.trimmed,
  }
}

/** A stage of the build's pipeline, as much of it as the log needs. */
export interface LogStage {
  id: string
  label: string
  core: boolean
  state: string
}

/** The lines of one step, or of none (id ""). */
export interface LogSection {
  id: string
  label: string
  /** The step's state; null for lines of no step the plan has. */
  state: string | null
  lines: LogLine[]
  /** Its lines, marks aside. */
  count: number
}

/**
 * A log's lines by step, in the plan's order: each factory step that has
 * lines, then lines of a step the plan does not have, then lines of none
 * ("Output": a factory that sends its log with its result says no step). A
 * mark goes with the line before it, so it stays where the lines went
 * missing.
 */
export function logSections(lines: readonly LogLine[], stages: readonly LogStage[]): LogSection[] {
  const byStep = new Map<string, LogSection>()
  let last = ""
  for (const l of lines) {
    const step = l.missing != null ? last : (l.step ?? "")
    last = step
    let s = byStep.get(step)
    if (!s) {
      const stage = stages.find((st) => !st.core && st.id === step)
      s = { id: step, label: stage?.label ?? (step || "Output"), state: stage?.state ?? null, lines: [], count: 0 }
      byStep.set(step, s)
    }
    s.lines.push(l)
    if (l.missing == null) s.count++
  }
  const order = (s: LogSection) => {
    const i = stages.findIndex((st) => !st.core && st.id === s.id)
    if (i >= 0) return i
    return s.id ? stages.length : stages.length + 1
  }
  return [...byStep.values()].sort((a, b) => order(a) - order(b))
}

/** What a mark says. */
export function missingText(n: number): string {
  if (n === 0) return "… some lines never reached core"
  return `… ${n} ${n === 1 ? "line" : "lines"} dropped by the factory`
}

/**
 * The log as text, to copy or download: each section under its label when
 * the log has steps, every line with its time.
 */
export function logText(log: BuildLogState, sections: readonly LogSection[]): string {
  const out: string[] = []
  if (log.trimmed > 0) out.push(`… ${log.trimmed} earlier lines dropped by core`)
  for (const s of sections) {
    if (log.live) out.push(`== ${s.label} ==`)
    for (const l of s.lines) {
      if (l.missing != null) out.push(missingText(l.missing))
      else out.push(l.at ? `${formatTime(l.at)}  ${l.text}` : l.text)
    }
  }
  return out.length ? out.join("\n") + "\n" : ""
}

/**
 * Whether a section shows its lines before the reader opens or closes it:
 * the step running or failed, or the only one.
 */
export function openByDefault(s: LogSection, sections: readonly LogSection[]): boolean {
  return sections.length === 1 || s.state === "RUNNING" || s.state === "FAILED"
}

import { useMemo, useRef, type KeyboardEvent } from "react"
import { LazyJsonEditor } from "@/components/ui/lazy-json-editor"
import { formatJson, indentJsonText } from "@/lib/json-edit"
import { cn } from "@/lib/utils"
import { useBuildRequest } from "@/graphql/hooks/builds"
import { buildRecordJson, sentNote, type BuildRecord } from "@/components/builds/build-record"

/** The JSON tab's views: the build to build again, or what went to the factory. */
export type BuildJsonView = "build" | "sent"

const VIEWS: readonly { id: BuildJsonView; label: string }[] = [
  { id: "build", label: "Build" },
  { id: "sent", label: "Sent to factory" },
]

/**
 * A build's JSON tab, in two views. Build: what it takes to build it again,
 * which pastes into a New build's JSON tab (build-record.ts). Sent to
 * factory: the factory.build request core sent, as it went (the build keeps
 * its bytes), to work on a factory away from core; shown indented, its
 * Download the bytes themselves.
 */
export function BuildJsonTab({
  build: b,
  view,
  onViewChange,
}: {
  build: BuildRecord
  view: BuildJsonView
  onViewChange: (view: BuildJsonView) => void
}) {
  const json = useMemo(() => formatJson(buildRecordJson(b)), [b])
  // Nothing went anywhere while it waits in the queue.
  const queued = b.state === "QUEUED"
  const sent = useBuildRequest(b.id, b.startedAt ?? null, view === "sent" && !queued)
  const request = sent.data?.build?.request ?? null
  const shown = useMemo(() => (request === null ? "" : indentJsonText(request)), [request])

  let note: string
  if (view === "build") note = "Paste it into a New build's JSON tab to build it again."
  else if (queued) note = "Not sent to a factory yet: core sends it once an instance takes it."
  else if (sent.isLoading) note = ""
  else if (sent.error) note = sent.error.message
  else if (request === null)
    note = "None kept: the build never reached a factory, or reached one before core kept what it sent."
  else note = sentNote(sent.data?.build?.instance ?? null, sent.data?.build?.startedAt ?? null)

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <ViewPicker value={view} onChange={onViewChange} />
        {note && <p className="text-sm text-muted-foreground">{note}</p>}
      </div>
      {view === "build" ? (
        <LazyJsonEditor
          value={json}
          onChange={noChange}
          readOnly
          fileName={`${b.id}.build.json`}
          aria-label="The build as JSON"
          className="flex-1 rounded-md border"
        />
      ) : (
        request !== null && (
          <LazyJsonEditor
            value={shown}
            onChange={noChange}
            readOnly
            fileName={`${b.id}.factory-build.json`}
            downloadText={request}
            aria-label="The factory.build request core sent"
            className="flex-1 rounded-md border"
          />
        )
      )}
    </div>
  )
}

/** The JSON tab only shows the build: nothing it could change. */
function noChange() {}

/** Build | Sent to factory, as one segmented control (arrow keys move between them). */
function ViewPicker({ value, onChange }: { value: BuildJsonView; onChange: (view: BuildJsonView) => void }) {
  const buttons = useRef(new Map<BuildJsonView, HTMLButtonElement>())
  function onKeyDown(e: KeyboardEvent) {
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0
    if (step === 0) return
    e.preventDefault()
    const at = VIEWS.findIndex((v) => v.id === value)
    const next = VIEWS[(at + step + VIEWS.length) % VIEWS.length].id
    onChange(next)
    buttons.current.get(next)?.focus()
  }
  return (
    <div
      role="radiogroup"
      aria-label="Show"
      className="flex gap-0.5 rounded-lg border bg-muted/40 p-0.5"
      onKeyDown={onKeyDown}
    >
      {VIEWS.map((v) => {
        const on = v.id === value
        return (
          <button
            key={v.id}
            ref={(el) => {
              if (el) buttons.current.set(v.id, el)
              else buttons.current.delete(v.id)
            }}
            type="button"
            role="radio"
            aria-checked={on}
            tabIndex={on ? 0 : -1}
            onClick={() => !on && onChange(v.id)}
            className={cn(
              "h-6.5 rounded-md px-3 text-sm transition-colors",
              on ? "bg-card shadow-sm ring-1 ring-border" : "text-muted-foreground hover:bg-card/60 hover:text-foreground",
            )}
          >
            {v.label}
          </button>
        )
      })}
    </div>
  )
}

import type { ReactNode } from "react"
import { DownloadIcon, ExternalLinkIcon, ListChecksIcon, PinIcon, RotateCcwIcon, Trash2Icon } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenuItem, ContextMenuSeparator } from "@/components/ui/context-menu"
import { ContextMenuRow } from "@/components/ui/context-menu-row"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable } from "@/components/ui/virtualized-data-table"
import type { BuildFieldsFragment } from "@/graphql/gql/graphql"
import { isUnfinished } from "@/graphql/hooks/builds"
import { useNow } from "@/hooks/use-now"
import { useRecordModals } from "@/hooks/use-record-modals"
import { artifactUrl } from "@/lib/build-file-upload"
import { ModuleIconCell } from "@/components/modules/module-icon"
import { cn } from "@/lib/utils"
import { PipelineDots } from "./build-pipeline"
import { currentStage, platformLabel } from "./build-values"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"

const GRID_COLS = "grid-cols-[minmax(0,1.4fr)_minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,0.8fr)_minmax(0,1fr)_48px]"
// While selecting: a checkbox column first (written out whole: Tailwind
// reads class names as they are in the source).
const GRID_COLS_SELECTING =
  "grid-cols-[16px_minmax(0,1.4fr)_minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,0.8fr)_minmax(0,1fr)_48px]"

/** The table's side of the page's selection for bulk actions. */
export interface BuildsTableSelection {
  /** Rows are being picked: a click picks one (shift: a range). */
  selecting: boolean
  picked: ReadonlySet<string>
  /** Every loaded row picked (or all matching). */
  allPicked: boolean
  onToggle: (id: string, shift: boolean) => void
  /** The header checkbox: every loaded row, or none. */
  onToggleAll: (on: boolean) => void
}

/**
 * An operation's builds, newest first. A row opens the build; its menu
 * (right-click, or ⋯) also downloads the artifact and starts a new build from
 * the same settings, as the build's page does, and deletes the build. Select
 * there starts picking rows for bulk actions (selection, the page's).
 */
export function BuildsTable({
  builds,
  icons,
  isLoading,
  isFetchingNextPage,
  hasNextPage,
  fetchNextPage,
  canWrite,
  onDelete,
  selection,
  emptyState,
}: {
  builds: BuildFieldsFragment[]
  icons: ReadonlyMap<string, ModuleIconSvgs>
  isLoading: boolean
  isFetchingNextPage: boolean
  hasNextPage: boolean
  fetchNextPage: () => void
  canWrite: boolean
  onDelete: (build: BuildFieldsFragment) => void
  selection: BuildsTableSelection
  emptyState: ReactNode
}) {
  const modals = useRecordModals()
  const now = useNow()
  const open = (b: BuildFieldsFragment) => modals.open({ kind: "build", id: b.id })
  const again = (b: BuildFieldsFragment) => modals.open({ kind: "new-build", factory: b.moduleName, from: b.id })
  const download = (b: BuildFieldsFragment) => {
    const a = document.createElement("a")
    a.href = artifactUrl(b.id)
    a.download = b.artifact?.filename ?? ""
    a.click()
  }

  const { selecting, picked } = selection
  const cols = selecting ? GRID_COLS_SELECTING : GRID_COLS

  return (
    <VirtualizedDataTable
      items={builds}
      isLoading={isLoading}
      isFetchingNextPage={isFetchingNextPage}
      hasNextPage={hasNextPage}
      fetchNextPage={fetchNextPage}
      gridCols={cols}
      entityNoun="builds"
      header={
        <>
          {selecting && (
            <Checkbox
              aria-label="Select every loaded build"
              className="self-center"
              checked={selection.allPicked}
              indeterminate={!selection.allPicked && picked.size > 0}
              onCheckedChange={(on) => selection.onToggleAll(on)}
            />
          )}
          <div>Factory · target</div>
          <div>Description</div>
          <div>Pipeline</div>
          <div>Platform</div>
          <div>Date</div>
          <div />
        </>
      }
      emptyState={emptyState}
      renderRow={(b) => {
        const at = currentStage(b.stages)
        return (
          <ContextMenuRow
            gridCols={cols}
            // While selecting, a click picks the row (shift: a range).
            onOpen={selecting ? (e) => selection.onToggle(b.id, e.shiftKey) : () => open(b)}
            className={cn(picked.has(b.id) && "bg-muted/60")}
            menuLabel="Build actions"
            menu={
              <>
                <ContextMenuItem onClick={() => open(b)}>
                  <ExternalLinkIcon className="size-4" />
                  Open
                </ContextMenuItem>
                {canWrite && (
                  <ContextMenuItem onClick={() => selection.onToggle(b.id, false)}>
                    <ListChecksIcon className="size-4" />
                    {picked.has(b.id) ? "Unselect" : "Select"}
                  </ContextMenuItem>
                )}
                {canWrite && b.artifact && !b.artifact.deleted && (
                  <ContextMenuItem onClick={() => download(b)}>
                    <DownloadIcon className="size-4" />
                    Download artifact
                  </ContextMenuItem>
                )}
                {canWrite && !isUnfinished(b.state) && (
                  <ContextMenuItem onClick={() => again(b)}>
                    <RotateCcwIcon className="size-4" />
                    Build again
                  </ContextMenuItem>
                )}
                {canWrite && (
                  <>
                    <ContextMenuSeparator />
                    <ContextMenuItem variant="destructive" onClick={() => onDelete(b)}>
                      <Trash2Icon className="size-4" />
                      Delete
                    </ContextMenuItem>
                  </>
                )}
              </>
            }
          >
            {(menuButton) => (
              <>
                {selecting && (
                  <Checkbox
                    aria-label={`Select build ${b.id}`}
                    className="pointer-events-none self-center bg-background"
                    checked={picked.has(b.id)}
                    tabIndex={-1}
                  />
                )}
                <ModuleIconCell
                  type="factory"
                  icon={icons.get(b.moduleName)}
                  title={
                    <>
                      <span className="truncate font-mono font-medium">{b.moduleName}</span>
                      <span className="truncate text-muted-foreground">&nbsp;· {b.target}</span>
                      {b.persistedSession && (
                        <span
                          className="ml-1.5 shrink-0 self-center text-muted-foreground"
                          title="Persists a session: its minions come back to it when they restart"
                        >
                          <PinIcon className="size-3" aria-label="Persists a session" />
                        </span>
                      )}
                    </>
                  }
                  subtitle={<span className="font-mono">{b.id}</span>}
                  subtitleTitle="Build id"
                />
                <div className="truncate text-sm" title={b.description || undefined}>
                  {b.description || <span className="text-muted-foreground">—</span>}
                </div>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <PipelineDots stages={b.stages} now={now} />
                  {/* Where the build is, or where it stopped and why. */}
                  {(at || b.error) && (
                    <span
                      className={cn("truncate text-xs", b.error || at?.state === "FAILED" ? "text-destructive" : "text-muted-foreground")}
                      title={b.error ? `${b.error.code}: ${b.error.message}` : undefined}
                    >
                      {[at?.label, b.error && `${b.error.code}: ${b.error.message}`].filter(Boolean).join(" · ")}
                    </span>
                  )}
                </div>
                <div className="truncate text-sm">{platformLabel(b.platform)}</div>
                <div className="truncate text-sm text-muted-foreground">
                  <FormattedDateTimeText date={b.createdAt} />
                  {b.createdBy.name && <span> by {b.createdBy.name}</span>}
                </div>
                <div>{menuButton}</div>
              </>
            )}
          </ContextMenuRow>
        )
      }}
    />
  )
}

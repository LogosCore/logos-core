import type { ReactNode } from "react"
import { useNavigate } from "react-router"
import { DownloadIcon, ExternalLinkIcon, RotateCcwIcon } from "lucide-react"
import { ContextMenuItem } from "@/components/ui/context-menu"
import { ContextMenuRow } from "@/components/ui/context-menu-row"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable } from "@/components/ui/virtualized-data-table"
import type { BuildFieldsFragment } from "@/graphql/gql/graphql"
import { isUnfinished } from "@/graphql/hooks/builds"
import { useNow } from "@/hooks/use-now"
import { artifactUrl } from "@/lib/build-file-upload"
import { ModuleIcon } from "@/components/modules/module-icon"
import { cn } from "@/lib/utils"
import { PipelineDots } from "./build-pipeline"
import { currentStage, platformLabel } from "./build-values"

const GRID_COLS = "grid-cols-[minmax(0,1.4fr)_minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,0.8fr)_minmax(0,1fr)_48px]"

/**
 * An operation's builds, newest first. A row opens the build; its menu
 * (right-click, or ⋯) also downloads the artifact and starts a new build from
 * the same settings, as the build's page does.
 */
export function BuildsTable({
  builds,
  icons,
  isLoading,
  canWrite,
  emptyState,
}: {
  builds: BuildFieldsFragment[]
  icons: ReadonlyMap<string, string>
  isLoading: boolean
  canWrite: boolean
  emptyState: ReactNode
}) {
  const navigate = useNavigate()
  const now = useNow()
  const open = (b: BuildFieldsFragment) => navigate(`/builds/${b.id}`)
  const again = (b: BuildFieldsFragment) => navigate(`/builds/new?kind=${encodeURIComponent(b.moduleName)}&from=${b.id}`)
  const download = (b: BuildFieldsFragment) => {
    const a = document.createElement("a")
    a.href = artifactUrl(b.id)
    a.download = b.artifact?.filename ?? ""
    a.click()
  }

  return (
    <VirtualizedDataTable
      items={builds}
      isLoading={isLoading}
      // The latest builds come in one page.
      isFetchingNextPage={false}
      hasNextPage={false}
      fetchNextPage={() => {}}
      gridCols={GRID_COLS}
      entityNoun="builds"
      header={
        <>
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
            gridCols={GRID_COLS}
            onOpen={() => open(b)}
            menuLabel="Build actions"
            menu={
              <>
                <ContextMenuItem onClick={() => open(b)}>
                  <ExternalLinkIcon className="size-4" />
                  Open
                </ContextMenuItem>
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
              </>
            }
          >
            {(menuButton) => (
              <>
                {/* The factory's icon as tall as the two lines beside it. */}
                <div className="flex min-w-0 items-center gap-3">
                  <ModuleIcon type="factory" icon={icons.get(b.moduleName)} className="size-9" />
                  <div className="flex min-w-0 flex-col">
                    <div className="flex min-w-0 items-baseline">
                      <span className="truncate font-mono">{b.moduleName}</span>
                      <span className="truncate text-muted-foreground">&nbsp;· {b.target}</span>
                    </div>
                    <div className="truncate font-mono text-xs text-muted-foreground" title="Build id">
                      {b.id}
                    </div>
                  </div>
                </div>
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

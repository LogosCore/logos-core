import type { ReactNode } from "react"
import { useNavigate } from "react-router"
import { EllipsisIcon, ExternalLinkIcon, PencilIcon, PlayIcon, Trash2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable, dataTableRowClass } from "@/components/ui/virtualized-data-table"
import type { BuildPresetFieldsFragment } from "@/graphql/gql/graphql"
import { KindName } from "@/components/modules/module-icon"
import { PRESETS_PATH } from "./build-access"
import { platformLabel } from "./build-values"

type Preset = BuildPresetFieldsFragment

const GRID_COLS = "grid-cols-[minmax(0,1.2fr)_minmax(0,1.3fr)_minmax(0,0.9fr)_minmax(0,1.2fr)_minmax(0,0.9fr)_minmax(0,0.8fr)_48px]"

const AMBER = "border-amber-600/40 text-amber-700 dark:text-amber-400"

/**
 * An operation's build presets. A row opens the preset (read-only without
 * write access); its menu starts a build from it, edits or deletes it.
 */
export function PresetsTable({
  presets,
  icons,
  isLoading,
  canWrite,
  emptyState,
  onDelete,
}: {
  presets: Preset[]
  icons: ReadonlyMap<string, string>
  isLoading: boolean
  canWrite: boolean
  emptyState: ReactNode
  onDelete: (preset: Preset) => void
}) {
  const navigate = useNavigate()
  const open = (p: Preset) => navigate(`${PRESETS_PATH}/${p.id}`)
  const build = (p: Preset) => navigate(`/builds/new?kind=${encodeURIComponent(p.moduleName)}&preset=${p.id}`)

  return (
    <VirtualizedDataTable
      items={presets}
      isLoading={isLoading}
      // An operation's presets come in one page.
      isFetchingNextPage={false}
      hasNextPage={false}
      fetchNextPage={() => {}}
      gridCols={GRID_COLS}
      entityNoun="presets"
      header={
        <>
          <div>Name</div>
          <div>Factory · target</div>
          <div>Platform</div>
          <div>Endpoints</div>
          <div>Status</div>
          <div>Updated</div>
          <div />
        </>
      }
      emptyState={emptyState}
      renderRow={(p) => {
        const values = Object.keys((p.values ?? {}) as Record<string, unknown>)
        return (
          <div
            className={`${dataTableRowClass(GRID_COLS)} cursor-pointer`}
            role="link"
            tabIndex={0}
            onClick={() => open(p)}
            onKeyDown={(e) => {
              if (e.key === "Enter") open(p)
            }}
          >
            <div className="min-w-0" title={values.length > 0 ? `Values: ${values.join(", ")}` : undefined}>
              <div className="truncate font-mono font-medium">{p.name}</div>
              {p.description && <div className="truncate text-xs text-muted-foreground">{p.description}</div>}
            </div>
            <div className="flex min-w-0 items-center">
              <KindName type="factory" name={p.moduleName} icon={icons.get(p.moduleName)} />
              <span className="truncate text-muted-foreground">&nbsp;· {p.target}</span>
            </div>
            <div className="truncate text-sm">
              {p.platform ? platformLabel(p.platform) : <span className="text-muted-foreground">When building</span>}
            </div>
            <div className="truncate font-mono text-xs" title={p.connections.map((c) => c.endpointName ?? "(deleted)").join(" → ")}>
              {p.connections.length === 0 ? (
                <span className="font-sans text-sm text-muted-foreground">When building</span>
              ) : (
                p.connections.map((c) => c.endpointName ?? "(deleted)").join(" → ")
              )}
            </div>
            <StatusCell preset={p} />
            <div className="truncate text-sm text-muted-foreground" title={p.updatedBy?.name ? `by ${p.updatedBy.name}` : undefined}>
              <FormattedDateTimeText date={p.updatedAt} />
            </div>
            {/* The menu sits inside the clickable row: keep its clicks and
                keys from also opening the preset. */}
            <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
              <DropdownMenu>
                <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Preset actions" />}>
                  <EllipsisIcon className="size-4" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  {canWrite ? (
                    <>
                      <DropdownMenuItem onClick={() => build(p)}>
                        <PlayIcon className="size-4" />
                        Build from preset
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onClick={() => open(p)}>
                        <PencilIcon className="size-4" />
                        Edit
                      </DropdownMenuItem>
                      <DropdownMenuItem variant="destructive" onClick={() => onDelete(p)}>
                        <Trash2Icon className="size-4" />
                        Delete
                      </DropdownMenuItem>
                    </>
                  ) : (
                    <DropdownMenuItem onClick={() => open(p)}>
                      <ExternalLinkIcon className="size-4" />
                      Open
                    </DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        )
      }}
    />
  )
}

function StatusCell({ preset: p }: { preset: Preset }) {
  if (p.problems.length === 0 && !p.schemaChanged) {
    return <div className="text-sm text-muted-foreground">Ready</div>
  }
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      {p.problems.length > 0 && (
        <Badge
          variant="outline"
          className={AMBER}
          title={`Doesn't fit the factory's build settings any more; a build from it needs fixing:\n${p.problems
            .map((pr) => (pr.key ? `${pr.key}: ${pr.message}` : pr.message))
            .join("\n")}`}
        >
          Doesn't fit
        </Badge>
      )}
      {p.schemaChanged && (
        <Badge
          variant="outline"
          className={AMBER}
          title="The factory now declares another version of its build settings. Check the preset and save it again."
        >
          Saved for v{p.schemaVersion}
        </Badge>
      )}
    </div>
  )
}

import type { ReactNode } from "react"
import { useNavigate } from "react-router"
import { CopyIcon, DownloadIcon, PencilIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { ContextMenuItem, ContextMenuSeparator } from "@/components/ui/context-menu"
import { ContextMenuRow } from "@/components/ui/context-menu-row"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { VirtualizedDataTable } from "@/components/ui/virtualized-data-table"
import type { ChannelEndpointFieldsFragment } from "@/graphql/gql/graphql"
import { fetchChannelEndpointExport } from "@/graphql/hooks/channel-endpoints"
import { errorMessage } from "@/components/modules/management/api-errors"
import { KindName } from "@/components/modules/module-icon"
import { exportFileName, settingRows, type Kind } from "./endpoint-values"

type Endpoint = ChannelEndpointFieldsFragment

const GRID_COLS = "grid-cols-[minmax(0,1.2fr)_minmax(0,0.6fr)_minmax(0,2.2fr)_minmax(0,0.9fr)_minmax(0,0.8fr)_48px]"

const AMBER = "border-amber-600/40 text-amber-700 dark:text-amber-400"

/**
 * The scoped operation's channel endpoints, one row each. A row opens the
 * endpoint (read-only without write access); its menu (right-click, or ⋯)
 * copies or downloads the connection settings as JSON with every default
 * filled in, edits or deletes.
 */
export function EndpointsTable({
  endpoints,
  kinds,
  isLoading,
  canWrite,
  emptyState,
  onDelete,
}: {
  endpoints: Endpoint[]
  kinds: Map<string, Kind>
  isLoading: boolean
  canWrite: boolean
  emptyState: ReactNode
  onDelete: (endpoint: Endpoint) => void
}) {
  const navigate = useNavigate()
  const open = (ep: Endpoint) => navigate(`/endpoints/${ep.id}`)

  return (
    <VirtualizedDataTable
      items={endpoints}
      isLoading={isLoading}
      // An operation's endpoints come in one page.
      isFetchingNextPage={false}
      hasNextPage={false}
      fetchNextPage={() => {}}
      gridCols={GRID_COLS}
      entityNoun="endpoints"
      header={
        <>
          <div>Name</div>
          <div>Channel</div>
          <div>Settings</div>
          <div>Status</div>
          <div>Updated</div>
          <div />
        </>
      }
      emptyState={emptyState}
      renderRow={(ep) => (
        <ContextMenuRow
          gridCols={GRID_COLS}
          onOpen={() => open(ep)}
          menuLabel="Endpoint actions"
          menu={endpointMenu(ep, canWrite, () => open(ep), () => onDelete(ep))}
        >
          {(menuButton) => (
            <>
              <div className="min-w-0">
                <div className="truncate font-mono font-medium">{ep.name}</div>
                {ep.description && <div className="truncate text-xs text-muted-foreground">{ep.description}</div>}
              </div>
              <div className="flex min-w-0 items-center gap-1.5">
                <KindName type="channel" name={ep.moduleName} icon={kinds.get(ep.moduleName)?.icon} />
                <span className="text-xs text-muted-foreground">v{ep.schemaVersion}</span>
              </div>
              <SettingsCell endpoint={ep} kind={kinds.get(ep.moduleName) ?? null} />
              <StatusCell endpoint={ep} />
              <div className="truncate text-sm text-muted-foreground" title={ep.updatedBy?.name ? `by ${ep.updatedBy.name}` : undefined}>
                <FormattedDateTimeText date={ep.updatedAt} />
              </div>
              <div>{menuButton}</div>
            </>
          )}
        </ContextMenuRow>
      )}
    />
  )
}

function SettingsCell({ endpoint, kind }: { endpoint: Endpoint; kind: Kind | null }) {
  const rows = settingRows(kind, (endpoint.values ?? {}) as Record<string, unknown>)
  if (rows.length === 0) {
    return <div className="truncate text-sm text-muted-foreground">Channel defaults</div>
  }
  return (
    <div className="truncate text-sm" title={rows.map((r) => `${r.label}: ${r.text}`).join("\n")}>
      {rows.map((r, i) => (
        <span key={r.key}>
          {i > 0 && <span className="text-muted-foreground"> · </span>}
          <span className="text-muted-foreground">{r.label}</span> <span className="font-mono text-xs">{r.text}</span>
        </span>
      ))}
    </div>
  )
}

function StatusCell({ endpoint: ep }: { endpoint: Endpoint }) {
  if (ep.problems.length === 0 && !ep.schemaChanged) {
    return <div className="text-sm text-muted-foreground">Ready</div>
  }
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      {ep.problems.length > 0 && (
        <Badge
          variant="outline"
          className={AMBER}
          title={`Doesn't fit the channel's connection settings, so it can't be used:\n${ep.problems
            .map((p) => (p.key ? `${p.key}: ${p.message}` : p.message))
            .join("\n")}`}
        >
          Doesn't fit
        </Badge>
      )}
      {ep.schemaChanged && (
        <Badge
          variant="outline"
          className={AMBER}
          title="The channel now declares another version of its connection settings. Check the values and save again."
        >
          Saved for v{ep.schemaVersion}
        </Badge>
      )}
    </div>
  )
}

/** An endpoint's menu: export it, and for writers edit or delete it. */
function endpointMenu(ep: Endpoint, canWrite: boolean, onEdit: () => void, onDelete: () => void): ReactNode {
  // An endpoint that doesn't fit its channel's settings can't be exported.
  const exportable = ep.problems.length === 0
  return (
    <>
      <ContextMenuItem onClick={() => void exportEndpoint(ep, "copy")} disabled={!exportable}>
        <CopyIcon className="size-4" />
        Copy as JSON
      </ContextMenuItem>
      <ContextMenuItem onClick={() => void exportEndpoint(ep, "download")} disabled={!exportable}>
        <DownloadIcon className="size-4" />
        Download as JSON
      </ContextMenuItem>
      {canWrite && (
        <>
          <ContextMenuSeparator />
          <ContextMenuItem onClick={onEdit}>
            <PencilIcon className="size-4" />
            Edit
          </ContextMenuItem>
          <ContextMenuItem variant="destructive" onClick={onDelete}>
            <Trash2Icon className="size-4" />
            Delete
          </ContextMenuItem>
        </>
      )}
    </>
  )
}

/** Copies or downloads an endpoint's connection settings, every default filled in. */
async function exportEndpoint(ep: Endpoint, mode: "copy" | "download") {
  let json: string
  try {
    json = JSON.stringify(await fetchChannelEndpointExport(ep.id), null, 2) + "\n"
  } catch (err) {
    toast.error("Could not get the endpoint", { description: errorMessage(err, "Request failed") })
    return
  }
  if (mode === "download") {
    const url = URL.createObjectURL(new Blob([json], { type: "application/json" }))
    const a = document.createElement("a")
    a.href = url
    a.download = exportFileName(ep.name)
    a.click()
    URL.revokeObjectURL(url)
    return
  }
  try {
    await navigator.clipboard.writeText(json)
    toast.success("Copied", { description: "Connection settings with every default filled in." })
  } catch {
    toast.error("Could not copy to the clipboard")
  }
}

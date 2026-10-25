import { useMemo, useState } from "react"
import { useNavigate } from "react-router"
import {
  ChevronDownIcon,
  CopyIcon,
  DownloadIcon,
  PencilIcon,
  PlusIcon,
  RadioTowerIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { SearchInput } from "@/components/ui/search-input"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useMyOperationRole } from "@/graphql/hooks/operations"
import type { ChannelEndpointFieldsFragment } from "@/graphql/gql/graphql"
import {
  fetchChannelEndpointExport,
  useChannelEndpoints,
  useChannelKinds,
  useDeleteChannelEndpoint,
} from "@/graphql/hooks/channel-endpoints"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { errorMessage } from "@/components/modules/management/api-errors"
import { formatValue } from "@/components/modules/management/field-values"
import {
  exportFileName,
  groupByKind,
  kindsByName,
  type Kind,
} from "@/components/endpoints/endpoint-values"

type Endpoint = ChannelEndpointFieldsFragment

/**
 * Channel endpoints of the scoped operation: ready-to-use minion connection
 * settings per channel kind, which its operators reuse when creating minions.
 * They change nothing by themselves: channels never see them, and a minion
 * keeps its own copy of the values.
 */
export function EndpointsPage() {
  const scopedOperation = useScopedOperation()

  usePageMetadata({
    title: scopedOperation ? `Endpoints · ${scopedOperation.name}` : "Endpoints",
    icon: { kind: "lucide", component: RadioTowerIcon },
  })

  // Endpoints belong to an operation; with none scoped there is nothing to
  // show, as on Tasks.
  if (!scopedOperation) return <GettingStartedPanel />
  return <EndpointsPageInner operationId={scopedOperation.id} />
}

function EndpointsPageInner({ operationId }: { operationId: string }) {
  const hasPermission = useAuthStore((s) => s.hasPermission)
  const { data: roleData } = useMyOperationRole(operationId)
  const role = roleData?.myOperationRole ?? null
  // App admins may write in any operation without being a member (the server
  // allows it); OPERATION_DELETE is what only they hold.
  const canWrite = role === "ADMIN" || role === "OPERATOR" || hasPermission(Permissions.OPERATION_DELETE)

  const kindsQuery = useChannelKinds()
  const { data, isLoading, error } = useChannelEndpoints(operationId)
  const [search, setSearch] = useState("")
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState<Endpoint | null>(null)
  const kinds = useMemo(() => kindsByName(kindsQuery.data?.channelKinds ?? []), [kindsQuery.data])

  const endpoints = useMemo(() => filterEndpoints(data?.channelEndpoints ?? [], search), [data, search])
  const groups = groupByKind(endpoints)
  const total = data?.channelEndpoints.length ?? 0

  return (
    <div className="flex flex-1 flex-col gap-4 p-4">
      {/* Toolbar, as on the other pages: search on the left, actions right. */}
      <div className="flex flex-wrap items-center gap-2">
        {total > 0 && (
          <SearchInput
            value={search}
            onValueChange={setSearch}
            placeholder="Search endpoints..."
            debounceMs={100}
            title=""
          />
        )}
        {canWrite && (
          <div className="ml-auto">
            <NewEndpointButton
              kinds={[...kinds.values()]}
              onPick={(kind) => navigate(`/endpoints/new?kind=${encodeURIComponent(kind)}`)}
            />
          </div>
        )}
      </div>

      {isLoading || kindsQuery.isLoading ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] gap-3">
          <Skeleton className="h-40" />
          <Skeleton className="h-40" />
        </div>
      ) : error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : total === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed py-12 text-center text-muted-foreground">
          <RadioTowerIcon className="size-8 opacity-50" />
          <p className="text-sm">No endpoints in this operation yet.</p>
          <p className="max-w-md text-xs">
            Save the addresses and settings minions should use to reach the channels — directly, or
            through a CDN or proxy in front of them — so everyone creating minions can reuse them.
          </p>
          {kinds.size === 0 && (
            <p className="max-w-md text-xs">No channel has declared its connection settings yet.</p>
          )}
        </div>
      ) : groups.length === 0 ? (
        <p className="py-12 text-center text-sm text-muted-foreground">No endpoints match “{search}”.</p>
      ) : (
        groups.map(([kindName, items]) => {
          const kind = kinds.get(kindName)
          return (
            <section key={kindName} className="flex flex-col gap-3">
              <h2 className="flex items-baseline gap-2 text-sm font-medium">
                <span className="font-mono">{kindName}</span>
                <span className="text-xs text-muted-foreground">
                  {kind ? `schema v${kind.version}` : "no connection schema"} · {items.length}
                </span>
              </h2>
              <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] gap-3">
                {items.map((ep) => (
                  <EndpointCard
                    key={ep.id}
                    endpoint={ep}
                    kind={kind ?? null}
                    canWrite={canWrite}
                    onEdit={() => navigate(`/endpoints/${ep.id}`)}
                    onDelete={() => setDeleting(ep)}
                  />
                ))}
              </div>
            </section>
          )
        })
      )}

      <DeleteEndpointDialog endpoint={deleting} onClose={() => setDeleting(null)} />
    </div>
  )
}

/** "New endpoint": straight to the form with one channel kind, a menu with several. */
function NewEndpointButton({ kinds, onPick }: { kinds: Kind[]; onPick: (kind: string) => void }) {
  if (kinds.length === 0) {
    return (
      <Button disabled title="No channel has declared its connection settings yet">
        <PlusIcon />
        New endpoint
      </Button>
    )
  }
  if (kinds.length === 1) {
    return (
      <Button onClick={() => onPick(kinds[0].name)}>
        <PlusIcon />
        New endpoint
      </Button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button>
            <PlusIcon />
            New endpoint
            <ChevronDownIcon />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        {kinds.map((k) => (
          <DropdownMenuItem key={k.name} onClick={() => onPick(k.name)}>
            <span className="font-mono">{k.name}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function EndpointCard({
  endpoint: ep,
  kind,
  canWrite,
  onEdit,
  onDelete,
}: {
  endpoint: Endpoint
  kind: Kind | null
  canWrite: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  const values = (ep.values ?? {}) as Record<string, unknown>
  const problems = ep.problems
  const [busy, setBusy] = useState(false)
  // Set values in schema order; without a schema (the kind is gone), the raw
  // keys.
  const rows: { key: string; label: string; text: string }[] = kind
    ? kind.fields
        .filter((f) => values[f.key] !== undefined)
        .map((f) => ({ key: f.key, label: f.label, text: formatValue(f.type, values[f.key], f.unit) }))
    : Object.entries(values).map(([key, v]) => ({ key, label: key, text: formatValue("string", v) }))
  const unset = kind ? kind.fields.length - rows.length : 0

  async function withJSON(handle: (json: string) => Promise<void> | void) {
    setBusy(true)
    try {
      const doc = await fetchChannelEndpointExport(ep.id)
      await handle(JSON.stringify(doc, null, 2) + "\n")
    } catch (err) {
      toast.error("Could not get the endpoint", { description: errorMessage(err, "Request failed") })
    } finally {
      setBusy(false)
    }
  }

  const download = () =>
    withJSON((json) => {
      const url = URL.createObjectURL(new Blob([json], { type: "application/json" }))
      const a = document.createElement("a")
      a.href = url
      a.download = exportFileName(ep.name)
      a.click()
      URL.revokeObjectURL(url)
    })

  const copy = () =>
    withJSON(async (json) => {
      await navigator.clipboard.writeText(json)
      toast.success("Copied", { description: "Connection settings with every default filled in." })
    })

  return (
    <div
      className={cn(
        "flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4",
        problems.length > 0 && "border-amber-500/50",
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono font-semibold">{ep.name}</span>
        {ep.schemaChanged && (
          <Badge
            variant="outline"
            className="border-amber-600/40 text-amber-700 dark:text-amber-400"
            title="The channel now declares another version of its connection settings. Check the values and save again."
          >
            Saved for schema v{ep.schemaVersion}
          </Badge>
        )}
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={copy}
            disabled={busy || problems.length > 0}
            title="Copy as JSON, defaults filled in"
          >
            <CopyIcon />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={download}
            disabled={busy || problems.length > 0}
            title="Download as JSON, defaults filled in"
          >
            <DownloadIcon />
          </Button>
          {canWrite && (
            <>
              <Button variant="ghost" size="icon-sm" onClick={onEdit} disabled={!kind} title="Edit">
                <PencilIcon />
              </Button>
              <Button variant="ghost" size="icon-sm" onClick={onDelete} title="Delete">
                <Trash2Icon />
              </Button>
            </>
          )}
        </div>
      </div>
      {ep.description && <p className="text-sm text-muted-foreground">{ep.description}</p>}

      {problems.length > 0 && (
        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 px-3 py-2 text-sm text-amber-800 dark:text-amber-300">
          <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
          <div className="flex flex-col gap-0.5">
            <span>Doesn't fit the channel's connection settings, so it can't be used:</span>
            <ul className="list-inside list-disc">
              {problems.map((p) => (
                <li key={p.key ?? ""}>
                  {p.key && <code className="font-mono">{p.key}</code>}
                  {p.key && ": "}
                  {p.message}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}

      <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
        {rows.map((r) => (
          <div key={r.key} className="contents">
            <dt className="truncate text-muted-foreground" title={r.key}>
              {r.label}
            </dt>
            <dd className="break-all font-mono text-xs leading-5">{r.text}</dd>
          </div>
        ))}
      </dl>
      {unset > 0 && <p className="text-xs text-muted-foreground">Other settings use the channel's defaults.</p>}
    </div>
  )
}

function DeleteEndpointDialog({ endpoint, onClose }: { endpoint: Endpoint | null; onClose: () => void }) {
  const del = useDeleteChannelEndpoint()
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    if (!endpoint) return
    setError(null)
    try {
      await del.mutateAsync(endpoint.id)
      toast.success(`Endpoint ${endpoint.name} deleted`)
      onClose()
    } catch (err) {
      setError(errorMessage(err, "Failed to delete the endpoint"))
    }
  }

  return (
    <Dialog
      open={endpoint !== null}
      onOpenChange={(o) => {
        if (!o && !del.isPending) {
          setError(null)
          onClose()
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Delete {endpoint?.name}?</DialogTitle>
          <DialogDescription>
            Nothing that already uses its settings changes: minions keep their own copy.
          </DialogDescription>
        </DialogHeader>
        {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={del.isPending}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={del.isPending}>
            {del.isPending ? "Deleting..." : "Delete"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Endpoints whose name, description, kind or values contain every term. */
function filterEndpoints(endpoints: readonly Endpoint[], query: string): Endpoint[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return [...endpoints]
  return endpoints.filter((ep) => {
    const text = [ep.name, ep.description, ep.moduleName, JSON.stringify(ep.values)]
      .filter(Boolean)
      .join(" ")
      .toLowerCase()
    return terms.every((t) => text.includes(t))
  })
}

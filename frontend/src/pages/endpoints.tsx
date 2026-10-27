import { useMemo, useState } from "react"
import { useNavigate } from "react-router"
import { ChevronDownIcon, FileUpIcon, PlusIcon, RadioTowerIcon } from "lucide-react"
import { toast } from "sonner"
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useMyOperationRole } from "@/graphql/hooks/operations"
import type { ChannelEndpointFieldsFragment } from "@/graphql/gql/graphql"
import { useChannelKinds, useDeleteChannelEndpoint, useInfiniteChannelEndpoints } from "@/graphql/hooks/channel-endpoints"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useEndpointStore } from "@/stores/endpoints"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { errorMessage } from "@/components/modules/management/api-errors"
import { EndpointsTable } from "@/components/endpoints/endpoints-table"
import { ImportEndpointDialog } from "@/components/endpoints/import-endpoint-dialog"
import { KindName } from "@/components/modules/module-icon"
import { kindsByName, type Kind } from "@/components/endpoints/endpoint-values"

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
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState<Endpoint | null>(null)
  const [importing, setImporting] = useState(false)
  const kinds = useMemo(() => kindsByName(kindsQuery.data?.channelKinds ?? []), [kindsQuery.data])

  // The server searches and filters (see the channelEndpoints query).
  const search = useEndpointStore((s) => s.search)
  const setSearch = useEndpointStore((s) => s.setSearch)
  const kindFilter = useEndpointStore((s) => s.kindFilter)
  const setKindFilter = useEndpointStore((s) => s.setKindFilter)
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useInfiniteChannelEndpoints({
    operationId,
    search: search.trim() || null,
    moduleNames: kindFilter ? [kindFilter] : null,
  })
  const endpoints = useConnectionNodes(data, (p) => p.channelEndpoints)
  const filtered = search.trim() !== "" || kindFilter !== null

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      {/* Toolbar, as on the other pages: search and filters on the left, actions right. */}
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput value={search} onValueChange={setSearch} placeholder="Search endpoints..." />
        <Select value={kindFilter ?? "all"} onValueChange={(v) => setKindFilter(v === "all" ? null : v)}>
          <SelectTrigger className="w-40" aria-label="Channel">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All channels</SelectItem>
            {[...kinds.values()].map((k) => (
              <SelectItem key={k.name} value={k.name}>
                <KindName type="channel" name={k.name} icon={k.icon} />
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {canWrite && (
          <div className="ml-auto flex items-center gap-2">
            <Button variant="outline" onClick={() => setImporting(true)} disabled={kinds.size === 0}>
              <FileUpIcon />
              Import
            </Button>
            <NewEndpointButton
              kinds={[...kinds.values()]}
              onPick={(kind) => navigate(`/endpoints/new?kind=${encodeURIComponent(kind)}`)}
            />
          </div>
        )}
      </div>

      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <EndpointsTable
          endpoints={endpoints}
          kinds={kinds}
          isLoading={isLoading || kindsQuery.isLoading}
          isFetchingNextPage={isFetchingNextPage}
          hasNextPage={hasNextPage}
          fetchNextPage={fetchNextPage}
          canWrite={canWrite}
          onDelete={setDeleting}
          emptyState={
            !filtered ? (
              <>
                <RadioTowerIcon className="size-8 opacity-50" />
                <p className="text-sm">No endpoints in this operation yet.</p>
                <p className="max-w-md text-center text-xs">
                  Save the addresses and settings minions should use to reach the channels — directly, or
                  through a CDN or proxy in front of them — so everyone creating minions can reuse them.
                </p>
                {kinds.size === 0 && (
                  <p className="max-w-md text-center text-xs">No channel has declared its connection settings yet.</p>
                )}
              </>
            ) : (
              <p className="text-sm">No endpoints match the search and filters.</p>
            )
          }
        />
      )}

      <DeleteEndpointDialog endpoint={deleting} onClose={() => setDeleting(null)} />
      <ImportEndpointDialog
        open={importing}
        onClose={() => setImporting(false)}
        kinds={kinds}
      />
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
            <KindName type="channel" name={k.name} icon={k.icon} />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
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

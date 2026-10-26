import { useMemo, useState } from "react"
import { useNavigate } from "react-router"
import { ChevronDownIcon, PlusIcon, RadioTowerIcon } from "lucide-react"
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
import { Permissions } from "@/constants/permissions"
import { useAuthStore } from "@/stores/auth"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useMyOperationRole } from "@/graphql/hooks/operations"
import type { ChannelEndpointFieldsFragment } from "@/graphql/gql/graphql"
import { useChannelEndpoints, useChannelKinds, useDeleteChannelEndpoint } from "@/graphql/hooks/channel-endpoints"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { errorMessage } from "@/components/modules/management/api-errors"
import { EndpointsTable } from "@/components/endpoints/endpoints-table"
import { groupByKind, kindsByName, type Kind } from "@/components/endpoints/endpoint-values"

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

  const endpoints = useMemo(
    () => groupByKind(filterEndpoints(data?.channelEndpoints ?? [], search)).flatMap(([, items]) => items),
    [data, search],
  )
  const total = data?.channelEndpoints.length ?? 0

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
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

      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <EndpointsTable
          endpoints={endpoints}
          kinds={kinds}
          isLoading={isLoading || kindsQuery.isLoading}
          canWrite={canWrite}
          onDelete={setDeleting}
          emptyState={
            total === 0 ? (
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
              <p className="text-sm">No endpoints match “{search}”.</p>
            )
          }
        />
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

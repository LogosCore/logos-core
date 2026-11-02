import { useState } from "react"
import { CrosshairIcon, PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SearchInput } from "@/components/ui/search-input"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import {
  useCheckMinionTarget,
  useConnectMinionTarget,
  useDisconnectMinionTarget,
  useInfiniteMinionTargets,
  useMinionTargetChangedSubscription,
  useRemoveMinionTarget,
} from "@/graphql/hooks/minion-targets"
import { TargetDialog, type BindConnection } from "./target-dialog"
import { TargetsTable } from "./targets-table"
import { TARGET_STATE_LABEL, type Target } from "./target-values"

/**
 * A build's targets (its modal's Targets tab): where its pages are, and
 * what calling them came to. Shown for builds with a bind connection.
 */
export function BuildTargets({
  operationId,
  buildId,
  connections,
  canWrite,
}: {
  operationId: string
  buildId: string
  connections: readonly BindConnection[]
  canWrite: boolean
}) {
  const [search, setSearch] = useState("")
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useInfiniteMinionTargets({
    operationId,
    buildId,
    search: search.trim() || null,
  })
  useMinionTargetChangedSubscription(operationId)
  const targets = useConnectionNodes(data, (p) => p.minionTargets)
  const check = useCheckMinionTarget()
  const connect = useConnectMinionTarget()
  const disconnect = useDisconnectMinionTarget()
  const [checking, setChecking] = useState<ReadonlySet<string>>(new Set())
  const [editing, setEditing] = useState<Target | null>(null)
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<Target | null>(null)
  const addable = connections.some((c) => !c.problem)

  async function checkNow(t: Target) {
    setChecking((s) => new Set(s).add(t.id))
    try {
      const res = await check.mutateAsync(t.id)
      const after = res.checkMinionTarget
      const label = TARGET_STATE_LABEL[after.state] ?? after.state
      if (after.state === "connected") toast.success(`${after.name}: ${label}`)
      else toast.warning(`${after.name}: ${label}`, { description: after.stateDetail ?? undefined })
    } catch (err) {
      toast.error("Could not call the target", { description: errorMessage(err, "Request failed") })
    } finally {
      setChecking((s) => {
        const next = new Set(s)
        next.delete(t.id)
        return next
      })
    }
  }

  async function connectOrNot(t: Target, open: boolean) {
    setChecking((s) => new Set(s).add(t.id))
    try {
      const after = open
        ? (await connect.mutateAsync(t.id)).connectMinionTarget
        : (await disconnect.mutateAsync(t.id)).disconnectMinionTarget
      const label = TARGET_STATE_LABEL[after.state] ?? after.state
      if (after.state === (open ? "connected" : "not_connected")) toast.success(`${after.name}: ${label}`)
      else toast.warning(`${after.name}: ${label}`, { description: after.stateDetail ?? undefined })
    } catch (err) {
      toast.error(open ? "Could not connect" : "Could not disconnect", { description: errorMessage(err, "Request failed") })
    } finally {
      setChecking((s) => {
        const next = new Set(s)
        next.delete(t.id)
        return next
      })
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput value={search} onValueChange={setSearch} placeholder="Search targets..." />
        {canWrite && (
          <Button
            className="ml-auto"
            onClick={() => setAdding(true)}
            disabled={!addable}
            title={addable ? undefined : connections.find((c) => c.problem)?.problem ?? undefined}
          >
            <PlusIcon />
            Add target
          </Button>
        )}
      </div>
      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <TargetsTable
          targets={targets}
          isLoading={isLoading}
          isFetchingNextPage={isFetchingNextPage}
          hasNextPage={hasNextPage}
          fetchNextPage={fetchNextPage}
          canWrite={canWrite}
          checking={checking}
          onCheck={checkNow}
          onConnect={(t, open) => void connectOrNot(t, open)}
          onEdit={setEditing}
          onRemove={setRemoving}
          emptyState={
            search.trim() === "" ? (
              <>
                <CrosshairIcon className="size-8 opacity-50" />
                <p className="text-sm">No targets yet.</p>
                <p className="max-w-md text-center text-xs">
                  This build's minion is reached by core, through its channel: put it in place, then add a target
                  with where it is. Core calls it (or connects to it) and its hello opens a session.
                </p>
              </>
            ) : (
              <p className="text-sm">No targets match the search.</p>
            )
          }
        />
      )}

      <TargetDialog
        open={adding || editing !== null}
        onClose={() => {
          setAdding(false)
          setEditing(null)
        }}
        buildId={buildId}
        connections={connections}
        target={editing}
      />
      <RemoveTargetDialog target={removing} onClose={() => setRemoving(null)} />
    </div>
  )
}

function RemoveTargetDialog({ target, onClose }: { target: Target | null; onClose: () => void }) {
  const remove = useRemoveMinionTarget()
  async function confirm() {
    if (!target) return
    try {
      await remove.mutateAsync(target.id)
      toast.success("Target removed")
      onClose()
    } catch (err) {
      toast.error("Could not remove the target", { description: errorMessage(err, "Request failed") })
    }
  }
  return (
    <Dialog open={target !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Remove target {target?.name}?</DialogTitle>
          <DialogDescription>
            Core stops calling it; the page stays on its server.
            {target?.session && (
              <>
                {" "}
                Session #{target.session.number} stays, with its history, showing no target: tasks queued for it wait. A
                target added to it later (the page moved) carries it on.
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={remove.isPending}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
            {remove.isPending ? "Removing..." : "Remove"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

import { useState } from "react"
import { toast } from "sonner"
import { HardDriveIcon, LoaderIcon, SparklesIcon } from "lucide-react"
import { errorMessage } from "@/components/modules/management/api-errors"
import { BucketCard, DiskCard, SeaweedFSCard, StorageWarnings } from "@/components/storage/storage-cards"
import { ActionsTable, ConsumersEmpty, ConsumersTable } from "@/components/storage/storage-tables"
import { bucketName, bucketWarnings, defaultBucket, purposeLabel, storeBytes } from "@/components/storage/storage-values"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Permissions } from "@/constants/permissions"
import { useGrowStorageBucket, useReclaimStorage, useStorageActions, useStorageConsumers, useStorageStatus } from "@/graphql/hooks/storage"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { useAuthStore } from "@/stores/auth"

type Tab = "overview" | "consumers" | "activity"

/**
 * The admins' Storage page (core: storage.graphql): the object store core
 * keeps its files in, as core read it at most a minute ago. Three tabs: the
 * overview (the store's warnings; SeaweedFS and its disks on the left, each
 * bucket with what core keeps there on the right), what core keeps by
 * operation, and the maintenance run (Reclaim space, Add a volume).
 */
export function StoragePage() {
  usePageMetadata({ title: "Storage", icon: { kind: "lucide", component: HardDriveIcon } })
  const canManage = useAuthStore((s) => s.hasPermission)(Permissions.STORAGE_MANAGE)
  const actionsQ = useStorageActions()
  const actions = useConnectionNodes(actionsQ.data, (p) => p.storageActions)
  const reclaiming = actions.some((a) => a.action === "reclaim" && a.status === "running")
  const statusQ = useStorageStatus(actions.some((a) => a.status === "running"))
  const status = statusQ.data?.storageStatus
  const reclaim = useReclaimStorage()
  const grow = useGrowStorageBucket()
  const [confirm, setConfirm] = useState<{ kind: "reclaim" } | { kind: "grow"; bucket: string } | null>(null)
  const [tab, setTab] = useState<Tab>("overview")
  const [picked, setPicked] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const coreBuckets = status?.buckets.filter((b) => b.purposes.length > 0) ?? []
  const bucket = picked ?? defaultBucket(coreBuckets)
  const q = search.trim()
  const consumersQ = useStorageConsumers(bucket, q)
  const consumers = useConnectionNodes(consumersQ.data, (p) => p.storageConsumers)
  const maintains = canManage && !!status?.seaweedfs.reachable

  const runReclaim = () =>
    void reclaim.mutateAsync().then(
      () => {
        toast.success("Reclaiming space: SeaweedFS compacts the volumes in the background. Activity says when it ends.")
        setTab("activity")
      },
      (err: unknown) => toast.error(errorMessage(err, "Could not reclaim space")),
    )
  const runGrow = (name: string) =>
    void grow.mutateAsync(name).then(
      (data) => {
        const a = data.growStorageBucket
        if (a.status === "succeeded") toast.success(`Added a volume to ${bucketName(name)}`)
        else toast.error(`Could not add a volume to ${bucketName(name)}: ${a.error ?? "it failed"}`)
      },
      (err: unknown) => toast.error(errorMessage(err, `Could not add a volume to ${bucketName(name)}`)),
    )

  return (
    // The lists' tabs hold the page to the viewport (min-h-0), their tables
    // filling what is left below the tab row; the overview's cards keep their
    // height, and the page scrolls.
    <div className={cn("flex flex-1 flex-col gap-2 p-2", tab !== "overview" && "min-h-0")}>
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className={cn("flex flex-col gap-2", tab !== "overview" && "min-h-0 flex-1")}>
        <div className="flex flex-wrap items-center gap-2">
          <TabsList>
            <TabsTrigger value="overview">
              <HardDriveIcon className="size-3.5" />
              Overview
            </TabsTrigger>
            <TabsTrigger value="consumers">What core keeps</TabsTrigger>
            <TabsTrigger value="activity">
              Activity
              {actionsQ.data && (
                <span className="text-muted-foreground tabular-nums">{actionsQ.data.pages[0]?.storageActions.totalCount}</span>
              )}
            </TabsTrigger>
          </TabsList>
          {tab === "consumers" && coreBuckets.length > 0 && (
            <>
              <Select value={bucket ?? ""} onValueChange={(v) => setPicked(String(v))}>
                <SelectTrigger className="w-64" aria-label="Bucket">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {coreBuckets.map((b) => (
                    <SelectItem key={b.name} value={b.name}>
                      {b.purposes.map(purposeLabel).join(", ")} · {formatBytes(b.objectBytes)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <SearchInput value={search} onValueChange={setSearch} placeholder="Search the operation, the kind..." />
            </>
          )}
        </div>

        <TabsContent value="overview" className="flex flex-none flex-col gap-4">
          {statusQ.error ? (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{statusQ.error.message}</div>
          ) : !status ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-48 w-full" />
            </div>
          ) : (
            <>
              <StorageWarnings warnings={status.warnings.filter((w) => w.bucket === null)} />
              {/* SeaweedFS and its disks on the left, the buckets on the right:
                  two of them side by side where the screen is wide. */}
              <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
                <div className="flex min-w-0 flex-col gap-4">
                  <SeaweedFSCard
                    status={status}
                    actions={
                      maintains && (
                        <Button
                          variant="ghost"
                          size="xs"
                          onClick={() => setConfirm({ kind: "reclaim" })}
                          disabled={reclaiming || reclaim.isPending}
                          title="Free the space deleted files still hold on disk, in every bucket"
                        >
                          {reclaiming ? <LoaderIcon className="animate-spin" /> : <SparklesIcon />}
                          {reclaiming ? "Reclaiming" : "Reclaim space"}
                        </Button>
                      )
                    }
                  />
                  {status.disks.map((d) => (
                    <DiskCard key={`${d.node}:${d.dir ?? ""}`} disk={d} store={storeBytes(status.buckets)} />
                  ))}
                </div>
                <div className="grid min-w-0 items-start gap-4 2xl:grid-cols-2">
                  {status.buckets.map((b) => (
                    <BucketCard
                      key={b.name}
                      bucket={b}
                      warnings={bucketWarnings(status.warnings, b.name)}
                      onGrow={maintains && b.name !== "" ? () => setConfirm({ kind: "grow", bucket: b.name }) : undefined}
                      growing={grow.isPending}
                    />
                  ))}
                </div>
              </div>
            </>
          )}
        </TabsContent>
        <TabsContent value="consumers" className="flex min-h-0 flex-col">
          <ConsumersTable
            consumers={consumers}
            isLoading={consumersQ.isLoading || statusQ.isLoading}
            isFetchingNextPage={consumersQ.isFetchingNextPage}
            hasNextPage={consumersQ.hasNextPage}
            fetchNextPage={() => void consumersQ.fetchNextPage()}
            emptyState={<ConsumersEmpty searching={q !== ""} />}
          />
        </TabsContent>
        <TabsContent value="activity" className="flex min-h-0 flex-col">
          <ActionsTable
            actions={actions}
            isLoading={actionsQ.isLoading}
            isFetchingNextPage={actionsQ.isFetchingNextPage}
            hasNextPage={actionsQ.hasNextPage}
            fetchNextPage={() => void actionsQ.fetchNextPage()}
          />
        </TabsContent>
      </Tabs>

      <Dialog open={confirm !== null} onOpenChange={(open) => !open && setConfirm(null)}>
        <DialogContent>
          {confirm?.kind === "reclaim" ? (
            <DialogHeader>
              <DialogTitle>Reclaim space</DialogTitle>
              <DialogDescription>
                SeaweedFS compacts every volume, in every bucket, whose deleted files hold a tenth of it or more: their space comes back
                to the disk. It runs in the background and rewrites those volumes, so the store answers slower until it ends. SeaweedFS
                also does this on its own, past 30%.
              </DialogDescription>
            </DialogHeader>
          ) : confirm?.kind === "grow" ? (
            <DialogHeader>
              <DialogTitle>Add a volume to {bucketName(confirm.bucket)}</DialogTitle>
              <DialogDescription>
                SeaweedFS adds a writable volume to the bucket: it takes one of the {status?.seaweedfs.freeSlots ?? 0} free volume
                slots. It gives a bucket with no writable volume one again, or room before much is written to it.
              </DialogDescription>
            </DialogHeader>
          ) : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirm(null)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                if (confirm?.kind === "reclaim") runReclaim()
                if (confirm?.kind === "grow") runGrow(confirm.bucket)
                setConfirm(null)
              }}
            >
              {confirm?.kind === "grow" ? "Add a volume" : "Reclaim space"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

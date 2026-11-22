import { useState } from "react"
import { toast } from "sonner"
import { HardDriveIcon, LoaderIcon, SparklesIcon } from "lucide-react"
import { errorMessage } from "@/components/modules/management/api-errors"
import { RetentionDialog } from "@/components/storage/retention-dialog"
import { BucketCard, CleanupCard, DiskCard, RetentionCard, SeaweedFSCard, StorageWarnings } from "@/components/storage/storage-cards"
import { ActionsTable, CleanupTable, ConsumersEmpty, ConsumersTable, type CleanupOperation } from "@/components/storage/storage-tables"
import {
  bucketName,
  bucketWarnings,
  daysText,
  DEFAULT_RETENTION_DAYS,
  defaultBucket,
  MAX_RETENTION_DAYS,
  parseDays,
  purposeLabel,
  recordingsText,
  storeBytes,
} from "@/components/storage/storage-values"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Permissions } from "@/constants/permissions"
import {
  useCleanUpStorage,
  useGrowStorageBucket,
  useReclaimStorage,
  useRefreshWhenEnded,
  useStorageActions,
  useStorageCleanupPreview,
  useStorageConsumers,
  useStorageRetention,
  useStorageStatus,
} from "@/graphql/hooks/storage"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useDebounced } from "@/hooks/use-debounced"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { formatBytes } from "@/lib/format-bytes"
import { cn } from "@/lib/utils"
import { useAuthStore } from "@/stores/auth"

type Tab = "overview" | "consumers" | "retention" | "activity"

/** What the confirm dialog asks for: a reclaim, a volume for a bucket, or a cleanup (of one operation's, or every one's). */
type Confirm =
  | { kind: "reclaim" }
  | { kind: "grow"; bucket: string }
  | { kind: "cleanup"; days: number; operation: CleanupOperation | null; recordings: number; bytes: number }

/**
 * The admins' Storage page (core: storage.graphql): the object store core
 * keeps its files in, as core read it at most a minute ago. Four tabs: the
 * overview (the store's warnings; SeaweedFS and its disks on the left, each
 * bucket with what core keeps there on the right), what core keeps by
 * operation, the retention cleanups of recordings (the policy, and a cleanup
 * now, previewed by operation), and the maintenance run (Reclaim space, Add
 * a volume, cleanups).
 */
export function StoragePage() {
  usePageMetadata({ title: "Storage", icon: { kind: "lucide", component: HardDriveIcon } })
  const canManage = useAuthStore((s) => s.hasPermission)(Permissions.STORAGE_MANAGE)
  const actionsQ = useStorageActions()
  const actions = useConnectionNodes(actionsQ.data, (p) => p.storageActions)
  const running = actions.some((a) => a.status === "running")
  const reclaiming = actions.some((a) => a.action === "reclaim" && a.status === "running")
  const cleaning = actions.some((a) => a.action === "cleanup" && a.status === "running")
  useRefreshWhenEnded(running)
  const statusQ = useStorageStatus(running)
  const status = statusQ.data?.storageStatus
  const reclaim = useReclaimStorage()
  const grow = useGrowStorageBucket()
  const cleanUp = useCleanUpStorage()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [editing, setEditing] = useState(false)
  const [tab, setTab] = useState<Tab>("overview")
  const [picked, setPicked] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const coreBuckets = status?.buckets.filter((b) => b.purposes.length > 0) ?? []
  const bucket = picked ?? defaultBucket(coreBuckets)
  const q = search.trim()
  const consumersQ = useStorageConsumers(bucket, q)
  const consumers = useConnectionNodes(consumersQ.data, (p) => p.storageConsumers)
  const maintains = canManage && !!status?.seaweedfs.reachable
  const held = tab === "consumers" || tab === "activity"

  // The retention tab: the policy, and what a cleanup at the age typed would
  // take now (read once the typing stops).
  const retentionQ = useStorageRetention(cleaning)
  const retention = retentionQ.data?.storageRetention
  const [days, setDays] = useState(String(DEFAULT_RETENTION_DAYS))
  const age = parseDays(days)
  const shownAge = useDebounced(age, 400)
  const previewQ = useStorageCleanupPreview(tab === "retention" && canManage ? shownAge : null, cleaning)
  const previewOps = previewQ.data?.pages.flatMap((p) => p.storageCleanupPreview.operations.edges.map((e) => e.node)) ?? []
  const preview = previewQ.data?.pages[0]?.storageCleanupPreview
  const totals =
    preview && shownAge === age && preview.olderThanDays === age
      ? { recordings: preview.recordings, bytes: preview.bytes, operations: preview.operations.totalCount }
      : null

  const runReclaim = () =>
    void reclaim.mutateAsync().then(
      () => {
        toast.success("Reclaiming space: SeaweedFS compacts the volumes in the background. Activity says when it ends.")
        setTab("activity")
      },
      (err: unknown) => toast.error(errorMessage(err, "Could not reclaim space")),
    )
  const runCleanup = (c: Extract<Confirm, { kind: "cleanup" }>) =>
    void cleanUp.mutateAsync({ olderThanDays: c.days, operationId: c.operation?.operationId ?? null }).then(
      () => {
        toast.success("Cleaning up recordings in the background. Activity says what it took when it ends.")
        setTab("activity")
      },
      (err: unknown) => toast.error(errorMessage(err, "Could not start the cleanup")),
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
    // filling what is left below the tab row; the overview's and the
    // retention's cards keep their height, and the page scrolls (the
    // retention's table has a height of its own).
    <div className={cn("flex flex-1 flex-col gap-2 p-2", held && "min-h-0")}>
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className={cn("flex flex-col gap-2", held && "min-h-0 flex-1")}>
        <div className="flex flex-wrap items-center gap-2">
          <TabsList>
            <TabsTrigger value="overview">
              <HardDriveIcon className="size-3.5" />
              Overview
            </TabsTrigger>
            <TabsTrigger value="consumers">What core keeps</TabsTrigger>
            <TabsTrigger value="retention">Retention</TabsTrigger>
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
        <TabsContent value="retention" className="flex flex-none flex-col gap-4">
          {retentionQ.error ? (
            <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{retentionQ.error.message}</div>
          ) : !retention ? (
            <Skeleton className="h-48 w-full" />
          ) : (
            <div className="grid items-start gap-4 lg:grid-cols-2">
              <RetentionCard retention={retention} onEdit={canManage ? () => setEditing(true) : undefined} />
              {canManage && (
                <CleanupCard
                  days={days}
                  onDays={setDays}
                  ageError={age === null ? `A whole number of days, 1 to ${MAX_RETENTION_DAYS.toLocaleString("en-US")}` : null}
                  totals={previewQ.error ? null : totals}
                  busy={cleaning}
                  onCleanUp={
                    age !== null && totals && totals.recordings > 0
                      ? () => setConfirm({ kind: "cleanup", days: age, operation: null, recordings: totals.recordings, bytes: totals.bytes })
                      : undefined
                  }
                />
              )}
            </div>
          )}
          {canManage &&
            (previewQ.error ? (
              <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{previewQ.error.message}</div>
            ) : (
              <div className="flex h-[28rem] flex-none flex-col">
                <CleanupTable
                  operations={age === null ? [] : previewOps}
                  isLoading={previewQ.isLoading}
                  isFetchingNextPage={previewQ.isFetchingNextPage}
                  hasNextPage={previewQ.hasNextPage}
                  fetchNextPage={() => void previewQ.fetchNextPage()}
                  busy={cleaning}
                  onCleanUp={
                    age !== null && totals
                      ? (o) => setConfirm({ kind: "cleanup", days: age, operation: o, recordings: o.recordings, bytes: o.bytes })
                      : undefined
                  }
                />
              </div>
            ))}
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
          ) : confirm?.kind === "cleanup" ? (
            <DialogHeader>
              <DialogTitle>
                {confirm.operation ? `Clean up ${confirm.operation.operationName ?? "an operation gone"}'s recordings` : "Clean up every operation's recordings"}
              </DialogTitle>
              <DialogDescription>
                Core deletes the files of the recordings that ended at least {daysText(confirm.days)} ago
                {confirm.operation ? ` in ${confirm.operation.operationName ?? "this operation"}` : ", in every operation"}:{" "}
                {recordingsText(confirm.recordings)}, {formatBytes(confirm.bytes)} now. Their records stay, marked cleaned up, and nothing
                is kept elsewhere; those of tasks marked important are kept. It runs in the background: Reclaim space then gives the disk
                the bytes back.
              </DialogDescription>
            </DialogHeader>
          ) : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirm(null)}>
              Cancel
            </Button>
            <Button
              variant={confirm?.kind === "cleanup" ? "destructive" : "default"}
              onClick={() => {
                if (confirm?.kind === "reclaim") runReclaim()
                if (confirm?.kind === "grow") runGrow(confirm.bucket)
                if (confirm?.kind === "cleanup") runCleanup(confirm)
                setConfirm(null)
              }}
            >
              {confirm?.kind === "grow" ? "Add a volume" : confirm?.kind === "cleanup" ? "Clean up" : "Reclaim space"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {retention && <RetentionDialog retention={retention} open={editing} onOpenChange={setEditing} />}
    </div>
  )
}

import { useEffect, useState } from "react"
import {
  CrosshairIcon,
  DownloadIcon,
  EllipsisIcon,
  HammerIcon,
  RotateCcwIcon,
  SquareIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react"
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
import {
  RECORD_TABS_LIST,
  RECORD_TABS_TRIGGER,
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
} from "@/components/ui/record-modal"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import { formatAbsolute } from "@/lib/relative-time"
import { useNow } from "@/hooks/use-now"
import { whenIdle } from "@/lib/preloadable"
import type { BuildTab } from "@/lib/record-modals"
import { artifactUrl } from "@/lib/build-file-upload"
import { useRecordModals } from "@/hooks/use-record-modals"
import {
  isUnfinished,
  useBuild,
  useCancelBuild,
  useDeleteBuildArtifact,
  useFactoryIcons,
  useFactoryKinds,
} from "@/graphql/hooks/builds"
import { errorMessage } from "@/components/modules/management/api-errors"
import { formatElapsed } from "@/components/modules/management/field-values"
import { useCanBuild } from "@/components/builds/build-access"
import { BuildOverview } from "@/components/builds/build-overview"
import { BuildCommands } from "@/components/builds/build-commands"
import { BuildConnections } from "@/components/builds/build-connections"
import { BuildRun } from "@/components/builds/build-run"
import { StageIcon } from "@/components/builds/build-pipeline"
import { BuildStateBadge } from "@/components/builds/build-state-badge"
import { buildElapsed, factoryKindsByName, platformLabel, recordedConnections } from "@/components/builds/build-values"
import { defaultBuildTab, type BuildRecord as Build } from "@/components/builds/build-record"
import { BuildJsonTab, type BuildJsonView } from "@/components/builds/build-json-tab"
import { ModuleIcon } from "@/components/modules/module-icon"
import { DeleteBuildDialog } from "@/components/builds/delete-build-dialog"
import { BuildTargets } from "@/components/targets/build-targets"
import { useBindConnections } from "@/graphql/hooks/minion-targets"

/**
 * One build (the build modal, ?build=<id>[&buildTab=]). Its header says what
 * it is (its description) and how it went, from every tab; the tabs under it:
 * Overview, what it is; Pipeline, how it went; Connections, how its minion
 * reaches its channels; Commands, what it can do (where its factory has
 * commands); Targets, for a build with a bind connection, where its pages
 * are; JSON, the build to build again or the request its factory got. Without
 * a tab in the URL it opens on its pipeline while that is where to look, on
 * its overview once it has succeeded.
 */
export function BuildDetails({ id, tab }: { id: string; tab: BuildTab | null }) {
  const { data, isLoading, error } = useBuild(id)
  const build = data?.build ?? null

  // Empty while loading, as the tables are.
  if (isLoading) return null
  if (error || !build) {
    return (
      <RecordModalMessage
        icon={<HammerIcon className="size-8 opacity-50" />}
        title="Build"
        text={error ? error.message : "This build does not exist."}
      />
    )
  }
  return <BuildDetail build={build} tab={tab} />
}

/** The build's state as its pipeline's stages show theirs, for the Pipeline tab's label. */
const STAGE_OF_STATE: Record<string, string> = {
  QUEUED: "PENDING",
  BUILDING: "RUNNING",
  SUCCEEDED: "SUCCEEDED",
  FAILED: "FAILED",
  CANCELLED: "CANCELLED",
}

function BuildDetail({ build: b, tab: urlTab }: { build: Build; tab: BuildTab | null }) {
  const { open, close } = useRecordModals()
  const icons = useFactoryIcons()
  const kinds = useFactoryKinds()
  const connections = recordedConnections(b).length
  // A tab for the minion's commands, unless neither it nor its factory has any.
  const hasCommands =
    b.commands.length > 0 || (factoryKindsByName(kinds.data?.factoryKinds ?? []).get(b.moduleName)?.commands.length ?? 0) > 0
  const { canWrite } = useCanBuild(b.operationId)
  const cancel = useCancelBuild()
  const [deleting, setDeleting] = useState(false)
  const [deletingBuild, setDeletingBuild] = useState(false)
  const running = isUnfinished(b.state)
  const artifact = b.artifact && !b.artifact.deleted ? b.artifact : null
  // The tab the URL names; else the one for the state the build was in when
  // the modal opened, kept, so a build finishing does not move the reader.
  const [firstTab] = useState(() => defaultBuildTab(b.state))
  const tab = urlTab ?? firstTab
  // The JSON tab's view, kept while the reader moves between tabs.
  const [jsonView, setJsonView] = useState<BuildJsonView>("build")
  // The title: the description's first line (the Overview shows a longer one whole).
  const title = b.description?.split("\n")[0].trim() ?? ""
  // A build whose minion only answers (a bind connection) has targets.
  const bind = useBindConnections(b.id)
  const bindConnections = bind.data ?? []
  const hasTargets = bindConnections.length > 0
  // Fetched once the modal has settled, so the JSON tab opens at once.
  useEffect(() => whenIdle(preloadJsonEditor), [])

  async function stop() {
    try {
      await cancel.mutateAsync(b.id)
      toast.success("Build cancelled")
    } catch (err) {
      toast.error("Could not cancel the build", { description: errorMessage(err, "Request failed") })
    }
  }

  return (
    <>
      <Tabs
        value={tab}
        onValueChange={(v) => {
          if (v === "overview" || v === "json" || v === "pipeline" || v === "connections" || v === "commands" || v === "targets") {
            open({ kind: "build", id: b.id, tab: v })
          }
        }}
        className="min-h-0 flex-1 gap-0"
      >
        <RecordModalHeader
          below={
            <TabsList className={RECORD_TABS_LIST}>
              <TabsTrigger value="overview" className={RECORD_TABS_TRIGGER}>
                Overview
                {/* The factory's warnings, flagged from every tab. */}
                {b.warnings.length > 0 && (
                  <span className="flex items-center gap-0.5 text-amber-600 tabular-nums dark:text-amber-400">
                    <TriangleAlertIcon aria-hidden className="size-3.5" />
                    {b.warnings.length}
                    <span className="sr-only">{b.warnings.length === 1 ? "warning" : "warnings"}</span>
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="pipeline" className={RECORD_TABS_TRIGGER}>
                {/* The build's state from every tab: spinning while it runs. */}
                <StageIcon state={STAGE_OF_STATE[b.state] ?? "PENDING"} className="size-3.5" />
                Pipeline
              </TabsTrigger>
              {connections > 0 && (
                <TabsTrigger value="connections" className={RECORD_TABS_TRIGGER}>
                  Connections
                  <span className="text-xs text-muted-foreground tabular-nums">{connections}</span>
                </TabsTrigger>
              )}
              {hasCommands && (
                <TabsTrigger value="commands" className={RECORD_TABS_TRIGGER}>
                  Commands
                  <span className="text-xs text-muted-foreground tabular-nums">{b.commands.length}</span>
                </TabsTrigger>
              )}
              {hasTargets && (
                <TabsTrigger value="targets" className={RECORD_TABS_TRIGGER}>
                  <CrosshairIcon className="size-3.5" />
                  Targets
                </TabsTrigger>
              )}
              <TabsTrigger value="json" className={RECORD_TABS_TRIGGER}>
                JSON
              </TabsTrigger>
            </TabsList>
          }
        >
          {/* The factory's icon as large as in the builds table, beside the
              build's description and, under it, what built it and how it went. */}
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <ModuleIcon type="factory" icon={icons.get(b.moduleName)} className="size-9" />
            <div className="flex min-w-0 flex-col">
              <RecordModalTitle>
                {title ? (
                  <span className="block truncate" title={b.description ?? undefined}>
                    {title}
                  </span>
                ) : (
                  <>
                    <span className="font-mono">{b.moduleName}</span> · {b.target}
                  </>
                )}
              </RecordModalTitle>
              <BuildHeadline build={b} />
            </div>
          </div>
          <div className="flex items-center gap-2">
            {canWrite && running && (
              <Button variant="outline" onClick={stop} disabled={cancel.isPending}>
                <SquareIcon />
                {cancel.isPending ? "Cancelling..." : "Cancel build"}
              </Button>
            )}
            {canWrite && !running && (
              <Button variant="outline" onClick={() => open({ kind: "new-build", factory: b.moduleName, from: b.id })}>
                <RotateCcwIcon />
                Build again
              </Button>
            )}
            {canWrite && artifact && (
              <Button nativeButton={false} render={<a href={artifactUrl(b.id)} download={artifact.filename} />}>
                <DownloadIcon />
                Download
              </Button>
            )}
            {canWrite && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button variant="ghost" size="icon" title="More" aria-label="More for this build">
                      <EllipsisIcon />
                    </Button>
                  }
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem variant="destructive" onClick={() => setDeletingBuild(true)}>
                    <Trash2Icon />
                    Delete build…
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </RecordModalHeader>

        <TabsContent value="overview" className="flex min-h-0 flex-col">
          <RecordModalBody>
            <BuildOverview build={b} canWrite={canWrite} onDeleteArtifact={() => setDeleting(true)} />
          </RecordModalBody>
        </TabsContent>

        {connections > 0 && (
          <TabsContent value="connections" className="flex min-h-0 flex-col">
            <RecordModalBody>
              <BuildConnections build={b} />
            </RecordModalBody>
          </TabsContent>
        )}

        {hasCommands && (
          <TabsContent value="commands" className="flex min-h-0 flex-col">
            <RecordModalBody>
              <BuildCommands build={b} />
            </RecordModalBody>
          </TabsContent>
        )}

        <TabsContent value="json" className="flex min-h-0 flex-col px-4 py-4">
          <BuildJsonTab build={b} view={jsonView} onViewChange={setJsonView} />
        </TabsContent>

        <TabsContent value="pipeline" className="flex min-h-0 flex-col">
          {/* The modal's full width: a long pipeline fits on one line instead of scrolling. */}
          <RecordModalBody>
            <BuildRun build={b} />
          </RecordModalBody>
        </TabsContent>

        {hasTargets && (
          // Padded as the JSON tab is, and no scrolling body: the table
          // scrolls itself, filling the tab's height.
          <TabsContent value="targets" className="flex min-h-0 flex-col px-4 py-4">
            <BuildTargets operationId={b.operationId} buildId={b.id} connections={bindConnections} canWrite={canWrite} />
          </TabsContent>
        )}
      </Tabs>

      <DeleteArtifactDialog build={deleting ? b : null} onClose={() => setDeleting(false)} />
      <DeleteBuildDialog build={deletingBuild ? b : null} onClose={() => setDeletingBuild(false)} onDeleted={close} />
    </>
  )
}

/**
 * Under the build's title: its factory, target and platform, its state, and
 * how long it took and when it ended (counting while it runs).
 */
function BuildHeadline({ build: b }: { build: Build }) {
  const kinds = useFactoryKinds()
  const now = useNow()
  const target = factoryKindsByName(kinds.data?.factoryKinds ?? [])
    .get(b.moduleName)
    ?.targets.find((t) => t.id === b.target)
  const running = isUnfinished(b.state)
  // Nothing to time while it waits in the queue.
  const elapsed = b.startedAt || !running ? formatElapsed(buildElapsed(b, now)) : null
  return (
    <div className="flex min-w-0 items-baseline gap-2 text-sm text-muted-foreground">
      <span className="truncate">
        <span className="font-mono">{b.moduleName}</span> factory · {target?.label ?? b.target} · {platformLabel(b.platform)}
      </span>
      <BuildStateBadge build={b} />
      {(elapsed || b.finishedAt) && (
        <span className="shrink-0 tabular-nums">
          {elapsed && (running ? `${elapsed} so far` : `took ${elapsed}`)}
          {b.finishedAt && ` · ${formatAbsolute(b.finishedAt)}`}
        </span>
      )}
    </div>
  )
}

function DeleteArtifactDialog({ build, onClose }: { build: Build | null; onClose: () => void }) {
  const del = useDeleteBuildArtifact()
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    if (!build) return
    setError(null)
    try {
      await del.mutateAsync(build.id)
      toast.success("Artifact deleted")
      onClose()
    } catch (err) {
      setError(errorMessage(err, "Failed to delete the artifact"))
    }
  }

  return (
    <Dialog
      open={build !== null}
      onOpenChange={(o) => {
        if (!o && !del.isPending) {
          setError(null)
          onClose()
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Delete the artifact?</DialogTitle>
          <DialogDescription>
            Its bytes are removed and cannot be downloaded again; the build record and the minion stay. A minion already
            deployed keeps working.
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

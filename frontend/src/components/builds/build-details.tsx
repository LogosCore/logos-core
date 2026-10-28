import { useEffect, useMemo, useState } from "react"
import { DownloadIcon, HammerIcon, RotateCcwIcon, SquareIcon } from "lucide-react"
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
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
} from "@/components/ui/record-modal"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import { formatJson } from "@/lib/json-edit"
import { whenIdle } from "@/lib/preloadable"
import type { BuildTab } from "@/lib/record-modals"
import { artifactUrl } from "@/lib/build-file-upload"
import { useRecordModals } from "@/hooks/use-record-modals"
import { isUnfinished, useBuild, useCancelBuild, useDeleteBuildArtifact, useFactoryIcons } from "@/graphql/hooks/builds"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useCanBuild } from "@/components/builds/build-access"
import { BuildOverview } from "@/components/builds/build-overview"
import { BuildRun } from "@/components/builds/build-run"
import { StageIcon } from "@/components/builds/build-pipeline"
import { buildRecordJson, defaultBuildTab, type BuildRecord as Build } from "@/components/builds/build-record"
import { ModuleIcon } from "@/components/modules/module-icon"

/**
 * One build (the build modal, ?build=<id>[&buildTab=]): Overview, what it is;
 * JSON, its record as the API gives it; Pipeline, how it went. Without a tab
 * in the URL it opens on its pipeline while that is where to look, on its
 * overview once it has succeeded.
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
  const { open } = useRecordModals()
  const icons = useFactoryIcons()
  const { canWrite } = useCanBuild(b.operationId)
  const cancel = useCancelBuild()
  const [deleting, setDeleting] = useState(false)
  const running = isUnfinished(b.state)
  const artifact = b.artifact && !b.artifact.deleted ? b.artifact : null
  // The tab the URL names; else the one for the state the build was in when
  // the modal opened, kept, so a build finishing does not move the reader.
  const [firstTab] = useState(() => defaultBuildTab(b.state))
  const tab = urlTab ?? firstTab
  const json = useMemo(() => formatJson(buildRecordJson(b)), [b])
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
          if (v === "overview" || v === "json" || v === "pipeline") open({ kind: "build", id: b.id, tab: v })
        }}
        className="min-h-0 flex-1 gap-0"
      >
        <RecordModalHeader>
          {/* The factory's icon as large as in the builds table, beside the title. */}
          <div className="flex min-w-0 items-center gap-3">
            <ModuleIcon type="factory" icon={icons.get(b.moduleName)} className="size-9" />
            <RecordModalTitle>
              <span className="font-mono">{b.moduleName}</span> · {b.target}
            </RecordModalTitle>
          </div>
          <div className="flex items-center gap-2 sm:ml-auto">
            <TabsList>
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="json">JSON</TabsTrigger>
              <TabsTrigger value="pipeline">
                {/* The build's state from every tab: spinning while it runs. */}
                <StageIcon state={STAGE_OF_STATE[b.state] ?? "PENDING"} className="size-3.5" />
                Pipeline
              </TabsTrigger>
            </TabsList>
            {canWrite && running && (
              <Button variant="outline" onClick={stop} disabled={cancel.isPending}>
                <SquareIcon />
                {cancel.isPending ? "Cancelling..." : "Cancel"}
              </Button>
            )}
            {canWrite && !running && (
              <Button variant="outline" onClick={() => open({ kind: "new-build", factory: b.moduleName, from: b.id })}>
                <RotateCcwIcon />
                Build again
              </Button>
            )}
            {canWrite && artifact && (
              <Button render={<a href={artifactUrl(b.id)} download={artifact.filename} />}>
                <DownloadIcon />
                Download
              </Button>
            )}
          </div>
        </RecordModalHeader>

        <TabsContent value="overview" className="flex min-h-0 flex-col">
          <RecordModalBody>
            <BuildOverview build={b} canWrite={canWrite} onDeleteArtifact={() => setDeleting(true)} />
          </RecordModalBody>
        </TabsContent>

        <TabsContent value="json" className="flex min-h-0 flex-col px-4 py-4">
          <LazyJsonEditor
            value={json}
            onChange={noChange}
            readOnly
            fileName={`${b.id}.build.json`}
            aria-label="The build as JSON"
            className="flex-1 rounded-md border"
          />
        </TabsContent>

        <TabsContent value="pipeline" className="flex min-h-0 flex-col">
          {/* The modal's full width: a long pipeline fits on one line instead of scrolling. */}
          <RecordModalBody>
            <BuildRun build={b} />
          </RecordModalBody>
        </TabsContent>
      </Tabs>

      <DeleteArtifactDialog build={deleting ? b : null} onClose={() => setDeleting(false)} />
    </>
  )
}

/** The JSON tab only shows the build: nothing it could change. */
function noChange() {}

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

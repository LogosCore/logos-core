import { useState } from "react"
import {
  CopyIcon,
  DownloadIcon,
  HammerIcon,
  RotateCcwIcon,
  SquareIcon,
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
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
} from "@/components/ui/record-modal"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"
import { formatBytes } from "@/lib/format-bytes"
import { formatAbsolute, relativeTime } from "@/lib/relative-time"
import { artifactUrl } from "@/lib/build-file-upload"
import { useNow } from "@/hooks/use-now"
import { useRecordModals } from "@/hooks/use-record-modals"
import type { BuildQuery } from "@/graphql/gql/graphql"
import { isUnfinished, useBuild, useCancelBuild, useDeleteBuildArtifact, useFactoryIcons } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { errorMessage } from "@/components/modules/management/api-errors"
import { formatElapsed, prettyJson } from "@/components/modules/management/field-values"
import { useCanBuild } from "@/components/builds/build-access"
import { BuildStateBadge } from "@/components/builds/build-state-badge"
import { buildConnectionRows, buildElapsed, platformLabel } from "@/components/builds/build-values"
import { BuildPipeline } from "@/components/builds/build-pipeline"
import { KindName, ModuleIcon } from "@/components/modules/module-icon"

type Build = NonNullable<BuildQuery["build"]>

/** One build (the build modal, ?build=<id>): its state while it runs, then its outcome. */
export function BuildDetails({ id }: { id: string }) {
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
  return <BuildDetail build={build} />
}

function BuildDetail({ build: b }: { build: Build }) {
  const { open } = useRecordModals()
  const now = useNow()
  const icons = useFactoryIcons()
  const channelIcons = useChannelIcons()
  const channelKinds = useChannelKinds()
  const connections = buildConnectionRows(b, kindsByName(channelKinds.data?.channelKinds ?? []))
  const { canWrite } = useCanBuild(b.operationId)
  const cancel = useCancelBuild()
  const [deleting, setDeleting] = useState(false)
  const running = isUnfinished(b.state)
  const artifact = b.artifact && !b.artifact.deleted ? b.artifact : null

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
      <RecordModalHeader>
        {/* The factory's icon as large as in the builds table, beside the title. */}
        <div className="flex min-w-0 items-center gap-3">
          <ModuleIcon type="factory" icon={icons.get(b.moduleName)} className="size-9" />
          <RecordModalTitle>
            <span className="font-mono">{b.moduleName}</span> · {b.target}
          </RecordModalTitle>
        </div>
        <div className="flex items-center gap-2 sm:ml-auto">
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

      <RecordModalBody>
      {/* The modal's full width: a long pipeline fits on one line instead of scrolling. */}
      <div className="flex min-w-0 flex-col gap-6 px-4 py-6">
        {b.error && (
          <div className="flex items-start gap-2 rounded-md bg-destructive/15 p-3 text-sm text-destructive">
            <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
            <div>
              <code className="font-mono font-medium">{b.error.code}</code>: {b.error.message}
            </div>
          </div>
        )}
        {/* What the factory warns about with its result. */}
        {b.warnings.length > 0 && (
          <div className="flex items-start gap-2 rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
            <TriangleAlertIcon aria-hidden className="mt-0.5 size-4 shrink-0" />
            <div className="flex min-w-0 flex-col gap-1">
              <p className="font-medium">Warnings</p>
              <ul className="list-inside list-disc">
                {b.warnings.map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            </div>
          </div>
        )}

        <Section title="Pipeline" aside={<BuildStateBadge build={b} />} className="dot-grid">
          <div className="py-3">
            <BuildPipeline stages={b.stages} now={now} />
          </div>
        </Section>

        <Section title="Overview">
          <dl className="grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)] gap-x-6 gap-y-2 py-3 text-sm">
            {b.description && (
              <Item label="Description">
                <span className="whitespace-pre-line">{b.description}</span>
              </Item>
            )}
            <Item label="Platform">{platformLabel(b.platform)}</Item>
            <Item label="Minion id">
              <span className="inline-flex items-center gap-1">
                <code className="font-mono text-xs">{b.id}</code>
                <Button variant="ghost" size="icon-sm" title="Copy" aria-label="Copy the minion id" onClick={() => copyToClipboard(b.id, "Minion id")}>
                  <CopyIcon />
                </Button>
              </span>
            </Item>
            <Item label="Factory instance">
              {b.instance ? (
                <>
                  <span className="font-mono">{b.instance}</span>
                  {b.factoryVersion && <span className="text-muted-foreground"> · {b.factoryVersion}</span>}
                </>
              ) : (
                <span className="text-muted-foreground">not taken yet</span>
              )}
            </Item>
            <Item label="Started">
              {formatAbsolute(b.createdAt)}
              {b.createdBy.name && <span className="text-muted-foreground"> by {b.createdBy.name}</span>}
            </Item>
            {b.finishedAt ? (
              <Item label="Finished">
                {formatAbsolute(b.finishedAt)} <span className="text-muted-foreground">· took {formatElapsed(buildElapsed(b))}</span>
                {b.cancelledBy?.name && <span className="text-muted-foreground"> · cancelled by {b.cancelledBy.name}</span>}
              </Item>
            ) : (
              <Item label="Deadline">
                {formatAbsolute(b.deadline)} <span className="text-muted-foreground">({relativeTime(b.deadline)})</span>
              </Item>
            )}
          </dl>
        </Section>

        {b.artifact && (
          <Section title="Artifact">
            <dl className="grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)] gap-x-6 gap-y-2 py-3 text-sm">
              <Item label="File">
                <span className="font-mono">{b.artifact.filename}</span>{" "}
                <span className="text-muted-foreground">· {formatBytes(b.artifact.size)}</span>
                {b.artifact.mediaType && <span className="text-muted-foreground"> · {b.artifact.mediaType}</span>}
              </Item>
              <Item label="SHA-256">
                <code className="break-all font-mono text-xs">{b.artifact.sha256}</code>
              </Item>
              <Item label="Capabilities">
                {b.capabilities.length === 0 ? (
                  <span className="text-muted-foreground">none reported</span>
                ) : (
                  <span className="flex flex-wrap gap-1">
                    {b.capabilities.map((c) => (
                      <Badge key={c} variant="secondary" className="font-mono">
                        {c}
                      </Badge>
                    ))}
                  </span>
                )}
              </Item>
            </dl>
            {b.artifact.deleted ? (
              <p className="pb-3 text-sm text-muted-foreground">The artifact's bytes were deleted; the build record stays.</p>
            ) : (
              canWrite && (
                <div className="pb-3">
                  <Button variant="outline" size="sm" onClick={() => setDeleting(true)}>
                    <Trash2Icon />
                    Delete the artifact
                  </Button>
                </div>
              )
            )}
          </Section>
        )}

        {b.log && (
          <Section title="Build log">
            <pre className="my-3 max-h-96 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-5 whitespace-pre-wrap">{b.log}</pre>
          </Section>
        )}

        {connections.length > 0 && (
          <Section title="Connections">
            <ol className="divide-y text-sm">
              {connections.map((c, i) => (
                <li key={i} className="flex min-w-0 flex-col gap-1 py-2.5">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="text-xs text-muted-foreground tabular-nums">{i + 1}.</span>
                    <KindName type="channel" name={c.moduleName} icon={channelIcons.get(c.moduleName)} className="shrink-0" />
                    <span className="text-muted-foreground">·</span>
                    <span className="truncate font-mono font-medium">{c.name}</span>
                    {c.address && <span className="truncate font-mono text-xs text-muted-foreground">{c.address}</span>}
                  </div>
                  {c.description && <p className="whitespace-pre-line text-muted-foreground">{c.description}</p>}
                </li>
              ))}
            </ol>
          </Section>
        )}

        <Section title="What the factory was given">
          <div className="flex flex-col gap-3 py-3">
            <p className="text-sm text-muted-foreground">
              Every value and connection setting with the defaults filled in, as the build was made. The
              connections' descriptions stay in core, so they are not here.
            </p>
            <pre className="max-h-96 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-5">
              {prettyJson({ values: b.values, connections: b.connections })}
            </pre>
          </div>
        </Section>
      </div>
      </RecordModalBody>

      <DeleteArtifactDialog build={deleting ? b : null} onClose={() => setDeleting(false)} />
    </>
  )
}

/**
 * A titled card; aside goes beside the title (the build's state, by the
 * pipeline), className onto the card (the pipeline's dotted canvas).
 */
function Section({
  title,
  aside,
  className,
  children,
}: {
  title: string
  aside?: React.ReactNode
  className?: string
  children: React.ReactNode
}) {
  return (
    <section>
      <div className="mb-1 flex items-center gap-2">
        <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{title}</h2>
        {aside}
      </div>
      <div className={cn("rounded-xl border bg-card px-4", className)}>{children}</div>
    </section>
  )
}

function Item({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </>
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

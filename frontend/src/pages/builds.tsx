import { useMemo, useState } from "react"
import { useLocation, useNavigate } from "react-router"
import {
  BookmarkIcon,
  ChevronDownIcon,
  HammerIcon,
  PencilIcon,
  PlayIcon,
  PlusIcon,
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
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { cn } from "@/lib/utils"
import { relativeTime } from "@/lib/relative-time"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import type { BuildPresetFieldsFragment } from "@/graphql/gql/graphql"
import {
  useBuildPresets,
  useBuilds,
  useDeleteBuildPreset,
  useFactoryIcons,
  useFactoryKinds,
} from "@/graphql/hooks/builds"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { BuildStateBadge } from "@/components/builds/build-state-badge"
import { BUILDS_PATH, PRESETS_PATH, useCanBuild } from "@/components/builds/build-access"
import { KindName } from "@/components/modules/module-icon"
import { errorMessage } from "@/components/modules/management/api-errors"
import { formatElapsed } from "@/components/modules/management/field-values"
import {
  buildElapsed,
  factoryKindsByName,
  platformLabel,
  type FactoryKind,
} from "@/components/builds/build-values"

/**
 * Minion builds of the scoped operation: each asks a factory for one minion
 * (a "Builds" tab, live while builds run), and presets keep build settings
 * ready to reuse ("Presets").
 */
export function BuildsPage() {
  const scopedOperation = useScopedOperation()
  const { pathname } = useLocation()
  const tab = pathname.startsWith(PRESETS_PATH) ? "presets" : "builds"

  usePageMetadata({
    title: scopedOperation ? `Builds · ${scopedOperation.name}` : "Builds",
    icon: { kind: "lucide", component: HammerIcon },
  })

  if (!scopedOperation) return <GettingStartedPanel />
  return <BuildsPageInner operationId={scopedOperation.id} tab={tab} />
}

function BuildsPageInner({ operationId, tab }: { operationId: string; tab: "builds" | "presets" }) {
  const navigate = useNavigate()
  const { canWrite } = useCanBuild(operationId)
  const kindsQuery = useFactoryKinds()
  const kinds = useMemo(() => [...factoryKindsByName(kindsQuery.data?.factoryKinds ?? []).values()], [kindsQuery.data])

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      <Tabs
        value={tab}
        onValueChange={(v) => navigate(v === "presets" ? PRESETS_PATH : BUILDS_PATH)}
        className="flex min-h-0 flex-1 flex-col gap-2"
      >
        <div className="flex flex-wrap items-center gap-2">
          <TabsList>
            <TabsTrigger value="builds">
              <HammerIcon className="size-3.5" />
              Builds
            </TabsTrigger>
            <TabsTrigger value="presets">
              <BookmarkIcon className="size-3.5" />
              Presets
            </TabsTrigger>
          </TabsList>
          {canWrite && (
            <div className="ml-auto">
              {tab === "builds" ? (
                <KindMenu
                  label="New build"
                  kinds={kinds}
                  needsInstance
                  onPick={(k) => navigate(`/builds/new?kind=${encodeURIComponent(k)}`)}
                />
              ) : (
                <KindMenu
                  label="New preset"
                  kinds={kinds}
                  onPick={(k) => navigate(`${PRESETS_PATH}/new?kind=${encodeURIComponent(k)}`)}
                />
              )}
            </div>
          )}
        </div>
        <TabsContent value="builds" className="flex min-h-0 flex-1 flex-col">
          <BuildsTab operationId={operationId} kindsLoading={kindsQuery.isLoading} noKinds={kinds.length === 0} />
        </TabsContent>
        <TabsContent value="presets" className="flex min-h-0 flex-1 flex-col">
          <PresetsTab operationId={operationId} canWrite={canWrite} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

/**
 * A factory kind to start from: straight to the form with one kind, a menu
 * with several. A build also needs a running instance of the kind.
 */
function KindMenu({
  label,
  kinds,
  needsInstance,
  onPick,
}: {
  label: string
  kinds: FactoryKind[]
  needsInstance?: boolean
  onPick: (kind: string) => void
}) {
  const usable = (k: FactoryKind) => !needsInstance || k.instances > 0
  if (kinds.length === 0) {
    return (
      <Button disabled title="No factory has declared what it builds yet">
        <PlusIcon />
        {label}
      </Button>
    )
  }
  if (kinds.length === 1) {
    const k = kinds[0]
    return (
      <Button
        onClick={() => onPick(k.name)}
        disabled={!usable(k)}
        title={usable(k) ? undefined : `No "${k.name}" factory is running`}
      >
        <PlusIcon />
        {label}
      </Button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button>
            <PlusIcon />
            {label}
            <ChevronDownIcon />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        {kinds.map((k) => (
          <DropdownMenuItem key={k.name} onClick={() => onPick(k.name)} disabled={!usable(k)}>
            <KindName type="factory" name={k.name} icon={k.icon} />
            {!usable(k) && <span className="ml-2 text-xs text-muted-foreground">not running</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// --- builds ----------------------------------------------------------------

function BuildsTab({ operationId, kindsLoading, noKinds }: { operationId: string; kindsLoading: boolean; noKinds: boolean }) {
  const navigate = useNavigate()
  const { data, isLoading, error } = useBuilds(operationId)
  const icons = useFactoryIcons()
  const builds = data?.builds ?? []

  if (isLoading || kindsLoading) return <Skeleton className="h-48" />
  if (error) return <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
  if (builds.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 rounded-xl border border-dashed py-12 text-center text-muted-foreground">
        <HammerIcon className="size-8 opacity-50" />
        <p className="text-sm">No builds in this operation yet.</p>
        <p className="max-w-md text-xs">
          A build asks a factory for one minion: pick what to build and the endpoints it connects through, and the
          factory builds it. Each build is a new minion with its own identity.
        </p>
        {noKinds && <p className="max-w-md text-xs">No factory has declared what it builds yet.</p>}
      </div>
    )
  }
  return (
    <div className="rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>State</TableHead>
            <TableHead>Factory · target</TableHead>
            <TableHead>Platform</TableHead>
            <TableHead>Minion</TableHead>
            <TableHead>Started</TableHead>
            <TableHead className="text-right">Took</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {builds.map((b) => (
            <TableRow key={b.id} className="cursor-pointer" onClick={() => navigate(`/builds/${b.id}`)}>
              <TableCell>
                <BuildStateBadge build={b} />
              </TableCell>
              <TableCell>
                <KindName type="factory" name={b.moduleName} icon={icons.get(b.moduleName)} />
                <span className="text-muted-foreground"> · {b.target}</span>
                {b.error && (
                  <span className="block max-w-md truncate text-xs text-destructive" title={b.error.message}>
                    {b.error.code}: {b.error.message}
                  </span>
                )}
              </TableCell>
              <TableCell className="text-sm">{platformLabel(b.platform)}</TableCell>
              <TableCell className="font-mono text-xs" title={b.minionId}>
                {b.minionId.slice(0, 8)}
              </TableCell>
              <TableCell className="text-sm text-muted-foreground">
                {relativeTime(b.createdAt)}
                {b.createdBy.name && <span> by {b.createdBy.name}</span>}
              </TableCell>
              <TableCell className="text-right text-sm text-muted-foreground">{formatElapsed(buildElapsed(b))}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

// --- presets ---------------------------------------------------------------

type Preset = BuildPresetFieldsFragment

function PresetsTab({ operationId, canWrite }: { operationId: string; canWrite: boolean }) {
  const navigate = useNavigate()
  const { data, isLoading, error } = useBuildPresets(operationId)
  const [deleting, setDeleting] = useState<Preset | null>(null)
  const icons = useFactoryIcons()
  const presets = data?.buildPresets ?? []

  if (isLoading) return <Skeleton className="h-40" />
  if (error) return <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
  return (
    <>
      {presets.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 rounded-xl border border-dashed py-12 text-center text-muted-foreground">
          <BookmarkIcon className="size-8 opacity-50" />
          <p className="text-sm">No presets in this operation yet.</p>
          <p className="max-w-md text-xs">
            A preset keeps build settings — a target, values and endpoints — so a build starts from them instead of an
            empty form. It follows its endpoints' changes; a build copies them.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] gap-3">
          {presets.map((p) => (
            <PresetCard
              key={p.id}
              preset={p}
              icon={icons.get(p.moduleName)}
              canWrite={canWrite}
              onBuild={() => navigate(`/builds/new?kind=${encodeURIComponent(p.moduleName)}&preset=${p.id}`)}
              onEdit={() => navigate(`${PRESETS_PATH}/${p.id}`)}
              onDelete={() => setDeleting(p)}
            />
          ))}
        </div>
      )}
      <DeletePresetDialog preset={deleting} onClose={() => setDeleting(null)} />
    </>
  )
}

function PresetCard({
  preset: p,
  icon,
  canWrite,
  onBuild,
  onEdit,
  onDelete,
}: {
  preset: Preset
  icon?: string
  canWrite: boolean
  onBuild: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const values = (p.values ?? {}) as Record<string, unknown>
  return (
    <div className={cn("flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4", p.problems.length > 0 && "border-amber-500/50")}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono font-semibold">{p.name}</span>
        {p.schemaChanged && (
          <Badge
            variant="outline"
            className="border-amber-600/40 text-amber-700 dark:text-amber-400"
            title="The factory now declares another version of its build settings. Check the preset and save it again."
          >
            Saved for schema v{p.schemaVersion}
          </Badge>
        )}
        {canWrite && (
          <div className="ml-auto flex items-center gap-1">
            <Button variant="ghost" size="icon-sm" onClick={onBuild} title="Build from this preset">
              <PlayIcon />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onEdit} title="Edit">
              <PencilIcon />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onDelete} title="Delete">
              <Trash2Icon />
            </Button>
          </div>
        )}
      </div>
      {p.description && <p className="text-sm text-muted-foreground">{p.description}</p>}
      {p.problems.length > 0 && (
        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 px-3 py-2 text-sm text-amber-800 dark:text-amber-300">
          <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
          <div className="flex flex-col gap-0.5">
            <span>Doesn't fit the factory's build settings any more; a build from it needs fixing:</span>
            <ul className="list-inside list-disc">
              {p.problems.map((pr) => (
                <li key={pr.key ?? ""}>
                  {pr.key && <code className="font-mono">{pr.key}</code>}
                  {pr.key && ": "}
                  {pr.message}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
      <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Factory</dt>
        <dd>
          <KindName type="factory" name={p.moduleName} icon={icon} /> · {p.target}
        </dd>
        <dt className="text-muted-foreground">Platform</dt>
        <dd>{p.platform ? platformLabel(p.platform) : <span className="text-muted-foreground">chosen when building</span>}</dd>
        <dt className="text-muted-foreground">Endpoints</dt>
        <dd className="font-mono text-xs leading-5">
          {p.connections.length === 0
            ? "chosen when building"
            : p.connections.map((c) => c.endpointName ?? "(deleted)").join(" → ")}
        </dd>
        {Object.keys(values).length > 0 && (
          <>
            <dt className="text-muted-foreground">Values</dt>
            <dd className="break-all font-mono text-xs leading-5">{Object.keys(values).join(", ")}</dd>
          </>
        )}
      </dl>
    </div>
  )
}

function DeletePresetDialog({ preset, onClose }: { preset: Preset | null; onClose: () => void }) {
  const del = useDeleteBuildPreset()
  const [error, setError] = useState<string | null>(null)

  async function confirm() {
    if (!preset) return
    setError(null)
    try {
      await del.mutateAsync(preset.id)
      toast.success(`Preset ${preset.name} deleted`)
      onClose()
    } catch (err) {
      setError(errorMessage(err, "Failed to delete the preset"))
    }
  }

  return (
    <Dialog
      open={preset !== null}
      onOpenChange={(o) => {
        if (!o && !del.isPending) {
          setError(null)
          onClose()
        }
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Delete {preset?.name}?</DialogTitle>
          <DialogDescription>Builds made from it are not affected: each kept its own copy of the settings.</DialogDescription>
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

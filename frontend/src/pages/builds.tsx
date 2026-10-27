import { useMemo, useState } from "react"
import { useLocation, useNavigate } from "react-router"
import { BookmarkIcon, ChevronDownIcon, HammerIcon, PlusIcon } from "lucide-react"
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
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
import { BUILDS_PATH, PRESETS_PATH, useCanBuild } from "@/components/builds/build-access"
import { BuildsTable } from "@/components/builds/builds-table"
import { PresetsTable } from "@/components/builds/presets-table"
import { KindName } from "@/components/modules/module-icon"
import { errorMessage } from "@/components/modules/management/api-errors"
import { factoryKindsByName, type FactoryKind } from "@/components/builds/build-values"

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
          <BuildsTab
            operationId={operationId}
            canWrite={canWrite}
            kindsLoading={kindsQuery.isLoading}
            noKinds={kinds.length === 0}
          />
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

function BuildsTab({
  operationId,
  canWrite,
  kindsLoading,
  noKinds,
}: {
  operationId: string
  canWrite: boolean
  kindsLoading: boolean
  noKinds: boolean
}) {
  const { data, isLoading, error } = useBuilds(operationId)
  const icons = useFactoryIcons()

  if (error) return <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
  return (
    <BuildsTable
      builds={data?.builds ?? []}
      icons={icons}
      isLoading={isLoading || kindsLoading}
      canWrite={canWrite}
      emptyState={
        <>
          <HammerIcon className="size-8 opacity-50" />
          <p className="text-sm">No builds in this operation yet.</p>
          <p className="max-w-md text-center text-xs">
            A build asks a factory for one minion: pick what to build and the connections it reaches its channels
            through, and the factory builds it. Each build is a new minion with its own identity.
          </p>
          {noKinds && <p className="max-w-md text-center text-xs">No factory has declared what it builds yet.</p>}
        </>
      }
    />
  )
}

// --- presets ---------------------------------------------------------------

type Preset = BuildPresetFieldsFragment

function PresetsTab({ operationId, canWrite }: { operationId: string; canWrite: boolean }) {
  const { data, isLoading, error } = useBuildPresets(operationId)
  const [deleting, setDeleting] = useState<Preset | null>(null)
  const icons = useFactoryIcons()

  if (error) return <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
  return (
    <>
      <PresetsTable
        presets={data?.buildPresets ?? []}
        icons={icons}
        isLoading={isLoading}
        canWrite={canWrite}
        onDelete={setDeleting}
        emptyState={
          <>
            <BookmarkIcon className="size-8 opacity-50" />
            <p className="text-sm">No presets in this operation yet.</p>
            <p className="max-w-md text-center text-xs">
              A preset keeps build settings — a target, values and connections — so a build starts from them instead
              of an empty form. Its connections hold their own settings: editing an endpoint later changes nothing
              here.
            </p>
          </>
        }
      />
      <DeletePresetDialog preset={deleting} onClose={() => setDeleting(null)} />
    </>
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

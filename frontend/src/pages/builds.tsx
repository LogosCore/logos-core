import { useMemo, useState } from "react"
import { ChevronDownIcon, HammerIcon, PlusIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { SearchInput } from "@/components/ui/search-input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useRecordModals } from "@/hooks/use-record-modals"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useFactoryIcons, useFactoryKinds, useInfiniteBuilds } from "@/graphql/hooks/builds"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useBuildStore } from "@/stores/builds"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { useCanBuild } from "@/components/builds/build-access"
import { BuildsTable } from "@/components/builds/builds-table"
import { DeleteBuildDialog, type DeletableBuild } from "@/components/builds/delete-build-dialog"
import { KindName } from "@/components/modules/module-icon"
import { factoryKindsByName, STATE_LABEL, type FactoryKind } from "@/components/builds/build-values"

/**
 * Minion builds of the scoped operation, live while builds run: each asks a
 * factory for one minion. An earlier build is the template for the next one
 * ("Build again" on its page).
 */
export function BuildsPage() {
  const scopedOperation = useScopedOperation()

  usePageMetadata({
    title: scopedOperation ? `Builds · ${scopedOperation.name}` : "Builds",
    icon: { kind: "lucide", component: HammerIcon },
  })

  if (!scopedOperation) return <GettingStartedPanel />
  return <BuildsPageInner operationId={scopedOperation.id} />
}

function BuildsPageInner({ operationId }: { operationId: string }) {
  const modals = useRecordModals()
  const [deleting, setDeleting] = useState<DeletableBuild | null>(null)
  const { canWrite } = useCanBuild(operationId)
  const kindsQuery = useFactoryKinds()
  const kinds = useMemo(() => [...factoryKindsByName(kindsQuery.data?.factoryKinds ?? []).values()], [kindsQuery.data])
  const icons = useFactoryIcons()
  // The server searches and filters (see the builds query).
  const search = useBuildStore((s) => s.search)
  const setSearch = useBuildStore((s) => s.setSearch)
  const stateFilter = useBuildStore((s) => s.stateFilter)
  const setStateFilter = useBuildStore((s) => s.setStateFilter)
  const factoryFilter = useBuildStore((s) => s.factoryFilter)
  const setFactoryFilter = useBuildStore((s) => s.setFactoryFilter)
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage, error } = useInfiniteBuilds({
    operationId,
    search: search.trim() || null,
    states: stateFilter ? [stateFilter] : null,
    moduleNames: factoryFilter ? [factoryFilter] : null,
  })
  const builds = useConnectionNodes(data, (p) => p.builds)
  const filtered = search.trim() !== "" || stateFilter !== null || factoryFilter !== null

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      {/* Toolbar, as on the other pages: search and filters on the left, actions right. */}
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput value={search} onValueChange={setSearch} placeholder="Search builds..." />
        <Select value={stateFilter ?? "all"} onValueChange={(v) => setStateFilter(v === "all" ? null : v)}>
          <SelectTrigger className="w-40" aria-label="State">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All states</SelectItem>
            {Object.entries(STATE_LABEL).map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={factoryFilter ?? "all"} onValueChange={(v) => setFactoryFilter(v === "all" ? null : v)}>
          <SelectTrigger className="w-40" aria-label="Factory">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All factories</SelectItem>
            {kinds.map((k) => (
              <SelectItem key={k.name} value={k.name}>
                <KindName type="factory" name={k.name} icon={k.icon} />
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {canWrite && (
          <div className="ml-auto">
            <NewBuildButton kinds={kinds} onPick={(k) => modals.open({ kind: "new-build", factory: k, from: null })} />
          </div>
        )}
      </div>
      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <BuildsTable
          builds={builds}
          icons={icons}
          isLoading={isLoading || kindsQuery.isLoading}
          isFetchingNextPage={isFetchingNextPage}
          hasNextPage={hasNextPage}
          fetchNextPage={fetchNextPage}
          canWrite={canWrite}
          onDelete={setDeleting}
          emptyState={
            !filtered ? (
              <>
                <HammerIcon className="size-8 opacity-50" />
                <p className="text-sm">No builds in this operation yet.</p>
                <p className="max-w-md text-center text-xs">
                  A build asks a factory for one minion: pick what to build and the connections it reaches its
                  channels through, and the factory builds it. Each build is a new minion with its own identity;
                  "Build again" on a build starts the next one from its settings.
                </p>
                {kinds.length === 0 && (
                  <p className="max-w-md text-center text-xs">No factory has declared what it builds yet.</p>
                )}
              </>
            ) : (
              <p className="text-sm">No builds match the search and filters.</p>
            )
          }
        />
      )}
      <DeleteBuildDialog build={deleting} onClose={() => setDeleting(null)} />
    </div>
  )
}

/**
 * The factory kind to build with: straight to the form with one kind, a menu
 * with several. A build needs a running instance of the kind.
 */
function NewBuildButton({ kinds, onPick }: { kinds: FactoryKind[]; onPick: (kind: string) => void }) {
  if (kinds.length === 0) {
    return (
      <Button disabled title="No factory has declared what it builds yet">
        <PlusIcon />
        New build
      </Button>
    )
  }
  if (kinds.length === 1) {
    const k = kinds[0]
    return (
      <Button
        onClick={() => onPick(k.name)}
        disabled={k.instances === 0}
        title={k.instances > 0 ? undefined : `No "${k.name}" factory is running`}
      >
        <PlusIcon />
        New build
      </Button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button>
            <PlusIcon />
            New build
            <ChevronDownIcon />
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        {kinds.map((k) => (
          <DropdownMenuItem key={k.name} onClick={() => onPick(k.name)} disabled={k.instances === 0}>
            <KindName type="factory" name={k.name} icon={k.icon} />
            {k.instances === 0 && <span className="ml-2 text-xs text-muted-foreground">not running</span>}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

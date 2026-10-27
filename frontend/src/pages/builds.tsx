import { useMemo, useState } from "react"
import { useNavigate } from "react-router"
import { ChevronDownIcon, HammerIcon, PlusIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { SearchInput } from "@/components/ui/search-input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useScopedOperation } from "@/hooks/use-scoped-operation"
import { useBuilds, useFactoryIcons, useFactoryKinds } from "@/graphql/hooks/builds"
import { GettingStartedPanel } from "@/components/onboarding/getting-started-panel"
import { BUILDS_PATH, useCanBuild } from "@/components/builds/build-access"
import { BuildsTable } from "@/components/builds/builds-table"
import { KindName } from "@/components/modules/module-icon"
import { factoryKindsByName, type FactoryKind } from "@/components/builds/build-values"

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
  const navigate = useNavigate()
  const { canWrite } = useCanBuild(operationId)
  const kindsQuery = useFactoryKinds()
  const kinds = useMemo(() => [...factoryKindsByName(kindsQuery.data?.factoryKinds ?? []).values()], [kindsQuery.data])
  // The server searches all of the operation's builds (see the builds query).
  const [search, setSearch] = useState("")
  const { data, isLoading, error } = useBuilds(operationId, search.trim())
  const icons = useFactoryIcons()
  const builds = data?.builds ?? []
  const searching = search.trim() !== ""

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      {/* Toolbar, as on the other pages: search on the left, actions right. */}
      {(builds.length > 0 || searching || canWrite) && (
        <div className="flex flex-wrap items-center gap-2">
          {(builds.length > 0 || searching) && (
            <SearchInput value={search} onValueChange={setSearch} placeholder="Search builds..." title="" />
          )}
          {canWrite && (
            <div className="ml-auto">
              <NewBuildButton kinds={kinds} onPick={(k) => navigate(`${BUILDS_PATH}/new?kind=${encodeURIComponent(k)}`)} />
            </div>
          )}
        </div>
      )}
      {error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error.message}</div>
      ) : (
        <BuildsTable
          builds={builds}
          icons={icons}
          isLoading={isLoading || kindsQuery.isLoading}
          canWrite={canWrite}
          emptyState={
            !searching ? (
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
              <p className="text-sm">No builds match “{search}”.</p>
            )
          }
        />
      )}
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

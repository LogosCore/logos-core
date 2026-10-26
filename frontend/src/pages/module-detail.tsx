import { Link, useParams, useSearchParams } from "react-router"
import { ArrowLeftIcon, BlocksIcon, PackageOpenIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useAuthStore } from "@/stores/auth"
import { Permissions } from "@/constants/permissions"
import { useModule, useModuleDetailSubscription } from "@/graphql/hooks/modules"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { ModuleHealthIndicator } from "@/components/modules/module-health-indicator"
import { ModuleIcon } from "@/components/modules/module-icon"
import { ModuleAdminDialogs, ModuleBlockButton } from "@/components/modules/module-admin-dialogs"
import { ModuleSyncIndicator } from "@/components/modules/management/module-sync-indicator"
import {
  BuildParameters,
  ConnectionSchema,
  ModuleIdentity,
  StatusValues,
} from "@/components/modules/management/overview-tab"
import { ConfigTab } from "@/components/modules/management/config-tab"
import { ActionsTab } from "@/components/modules/management/actions-tab"
import { ActivityTab } from "@/components/modules/management/activity-tab"
import { LogsTab } from "@/components/modules/management/logs-tab"

const TABS = ["overview", "configuration", "actions", "activity", "logs"] as const
type Tab = (typeof TABS)[number]

export function ModuleDetailPage() {
  const { instance = "" } = useParams<{ instance: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const tabParam = searchParams.get("tab")
  const tab: Tab = (TABS as readonly string[]).includes(tabParam ?? "") ? (tabParam as Tab) : "overview"

  usePageMetadata({
    title: instance ? `${instance} · Modules` : "Modules",
    icon: { kind: "lucide", component: BlocksIcon },
  })

  // Lifecycle, heartbeat and config-sync changes for this instance arrive on
  // moduleChanged and refetch the detail (the sync badge follows from that).
  useModuleDetailSubscription(instance)

  const hasPermission = useAuthStore((s) => s.hasPermission)
  const canWrite = hasPermission(Permissions.MODULE_CONFIG_WRITE)
  const canInvoke = hasPermission(Permissions.MODULE_ACTION_INVOKE)
  const canInvokeHigh = hasPermission(Permissions.MODULE_ACTION_INVOKE_HIGH)
  const canReadLogs = hasPermission(Permissions.MODULE_LOGS_READ)
  // Blocking and unblocking share the module:delete permission.
  const canAdmin = hasPermission(Permissions.MODULE_DELETE)

  const { data, isLoading, error, refetch } = useModule(instance)
  const module = data?.module
  const management = module?.management
  const connection = module?.connection
  const build = module?.build

  return (
    // On the Logs tab the page is held to the viewport (min-h-0): its list and
    // detail pane scroll inside themselves instead of growing the page.
    <div className={cn("flex flex-1 flex-col gap-4 p-4", tab === "logs" && "min-h-0")}>
      {/* Back link, identity and live status share one line; it wraps on
          narrow screens. Before the module loads only the back link shows. */}
      <header className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Link
            to="/modules"
            className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            <ArrowLeftIcon className="size-4" />
            Modules
          </Link>
          {module && (
            <>
              <span className="h-5 w-px bg-border" aria-hidden />
              <ModuleIcon type={module.type} icon={module.icon} className="size-8" />
              <h1 className="font-mono text-xl font-semibold">{module.instance}</h1>
              {/* What it is, as quiet text: details live on the Overview. */}
              <span className="text-sm text-muted-foreground">
                {[module.name, module.type, module.version && `v${module.version.replace(/^v/, "")}`]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
              {/* How it is: every state in the same dot-and-label style. */}
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 sm:ml-auto">
                <ModuleStatusIndicator status={module.status} lastHeartbeatAt={module.lastHeartbeatAt} />
                <ModuleHealthIndicator health={module.lastStatus} registered={module.status === "registered"} />
                {module.management && <ModuleSyncIndicator management={module.management} />}
                {canAdmin && <ModuleBlockButton module={module} />}
              </div>
            </>
          )}
        </div>
        {module?.description && <p className="text-sm">{module.description}</p>}
      </header>
      <ModuleAdminDialogs />

      {isLoading ? (
        <div className="flex flex-col gap-3">
          <Skeleton className="h-8 w-72" />
          <Skeleton className="h-5 w-96" />
          <Skeleton className="h-64 w-full" />
        </div>
      ) : error ? (
        <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">
          {error.message}
        </div>
      ) : !module ? (
        <div className="flex flex-col items-center gap-2 py-16 text-muted-foreground">
          <BlocksIcon className="size-8 opacity-50" />
          <p className="text-sm">
            No module instance <code className="font-mono">{instance}</code>.
          </p>
        </div>
      ) : (
        <>
          {!management ? (
            <div className="flex flex-col gap-4">
              <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed py-10 text-center text-muted-foreground">
                <PackageOpenIcon className="size-8 opacity-50" />
                <p className="text-sm">
                  This module doesn't declare any management capabilities.
                </p>
                <p className="max-w-md text-xs">
                  Modules that send a management manifest at registration get
                  configuration, actions and status here.
                </p>
              </div>
              <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <ModuleIdentity module={module} />
                {connection && <ConnectionSchema connection={connection} />}
                {build && <BuildParameters build={build} />}
              </div>
            </div>
          ) : (
            <Tabs
              // On the Logs tab it fills the page height, so the list takes
              // exactly what is left below its toolbar. Other tabs keep their
              // natural height and scroll as before.
              className={tab === "logs" ? "min-h-0 flex-1" : undefined}
              value={tab}
              onValueChange={(v) => {
                const next = new URLSearchParams(searchParams)
                if (v === "overview") next.delete("tab")
                else next.set("tab", String(v))
                setSearchParams(next, { replace: true })
              }}
            >
              <TabsList>
                <TabsTrigger value="overview">Overview</TabsTrigger>
                <TabsTrigger value="configuration">
                  Configuration
                  <span className="text-xs text-muted-foreground">
                    {management.properties.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="actions">
                  Actions
                  <span className="text-xs text-muted-foreground">
                    {management.actions.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="activity">Activity</TabsTrigger>
                {management.capabilities.includes("logs") && (
                  <TabsTrigger value="logs">Logs</TabsTrigger>
                )}
              </TabsList>
              <TabsContent value="overview" className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <ModuleIdentity module={module} />
                <StatusValues management={management} />
                {connection && <ConnectionSchema connection={connection} />}
                {build && <BuildParameters build={build} />}
              </TabsContent>
              {/* Kept mounted: unsaved edits survive a look at another tab. */}
              <TabsContent value="configuration" keepMounted>
                <ConfigTab
                  module={module}
                  management={management}
                  canWrite={canWrite}
                  refetch={refetch}
                />
              </TabsContent>
              <TabsContent value="actions">
                <ActionsTab
                  module={module}
                  management={management}
                  canInvoke={canInvoke}
                  canInvokeHigh={canInvokeHigh}
                />
              </TabsContent>
              <TabsContent value="activity">
                <ActivityTab module={module} management={management} />
              </TabsContent>
              {/* Mounted only while selected: leaving the tab ends the stream. */}
              {tab === "logs" && management.capabilities.includes("logs") && (
                <TabsContent value="logs" className="flex min-h-0 flex-col">
                  <LogsTab module={module} canRead={canReadLogs} />
                </TabsContent>
              )}
            </Tabs>
          )}
        </>
      )}
    </div>
  )
}

import { Link, useParams, useSearchParams } from "react-router"
import { ArrowLeftIcon, BlocksIcon, PackageOpenIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { useAuthStore } from "@/stores/auth"
import { Permissions } from "@/constants/permissions"
import { useModule, useModuleDetailSubscription } from "@/graphql/hooks/modules"
import { ModuleStatusIndicator } from "@/components/modules/module-status-indicator"
import { SyncStateBadge } from "@/components/modules/management/sync-state-badge"
import { ModuleIdentity, StatusValues } from "@/components/modules/management/overview-tab"
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

  const { data, isLoading, error, refetch } = useModule(instance)
  const module = data?.module

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
              <h1 className="font-mono text-xl font-semibold">{module.instance}</h1>
              <div className="flex flex-wrap items-center gap-2">
                {module.name && <Badge variant="default">{module.name}</Badge>}
                <Badge variant="secondary">{module.type}</Badge>
                {module.version && <Badge variant="outline">v{module.version.replace(/^v/, "")}</Badge>}
                {module.management && <SyncStateBadge management={module.management} />}
              </div>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
                <ModuleStatusIndicator status={module.status} />
                <span>Health: {module.lastStatus || "—"}</span>
                <span>
                  Last heartbeat:{" "}
                  {module.lastHeartbeatAt ? (
                    <FormattedDateTimeText date={module.lastHeartbeatAt} format="yyyy.mm.dd hh:MM:ss" />
                  ) : (
                    "never"
                  )}
                </span>
              </div>
            </>
          )}
        </div>
        {module?.description && <p className="text-sm">{module.description}</p>}
      </header>

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
          {!module.management ? (
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
              <ModuleIdentity module={module} />
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
                    {module.management.properties.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="actions">
                  Actions
                  <span className="text-xs text-muted-foreground">
                    {module.management.actions.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="activity">Activity</TabsTrigger>
                {module.management.capabilities.includes("logs") && (
                  <TabsTrigger value="logs">Logs</TabsTrigger>
                )}
              </TabsList>
              <TabsContent value="overview" className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <ModuleIdentity module={module} />
                <StatusValues management={module.management} />
              </TabsContent>
              <TabsContent value="configuration">
                <ConfigTab
                  module={module}
                  management={module.management}
                  canWrite={canWrite}
                  refetch={refetch}
                />
              </TabsContent>
              <TabsContent value="actions">
                <ActionsTab
                  module={module}
                  management={module.management}
                  canInvoke={canInvoke}
                  canInvokeHigh={canInvokeHigh}
                />
              </TabsContent>
              <TabsContent value="activity">
                <ActivityTab module={module} management={module.management} />
              </TabsContent>
              {/* Mounted only while selected: leaving the tab ends the stream. */}
              {tab === "logs" && module.management.capabilities.includes("logs") && (
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

import { BlocksIcon } from "lucide-react"
import { useInfiniteModules, useModuleChangedSubscription } from "@/graphql/hooks/modules"
import { useConnectionNodes } from "@/hooks/use-connection-nodes"
import { useModuleStore } from "@/stores/modules"
import { usePageMetadata } from "@/hooks/use-page-metadata"
import { ModulesToolbar } from "@/components/modules/modules-toolbar"
import { ModulesTable } from "@/components/modules/modules-table"
import { ModuleAdminDialogs } from "@/components/modules/module-admin-dialogs"

export function ModulesPage() {
  usePageMetadata({
    title: "Modules",
    icon: { kind: "lucide", component: BlocksIcon },
  })

  // Real-time: a module registering, deregistering, dying, being blocked or
  // deleted refetches the list.
  useModuleChangedSubscription()

  // The server searches and filters (see the modules query).
  const search = useModuleStore((s) => s.search)
  const statusFilter = useModuleStore((s) => s.statusFilter)
  const typeFilter = useModuleStore((s) => s.typeFilter)
  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteModules({
    search: search.trim() || null,
    status: statusFilter ? [statusFilter] : null,
    type: typeFilter ? [typeFilter] : null,
  })
  const modules = useConnectionNodes(data, (p) => p.modules)

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      <ModulesToolbar />
      <ModulesTable
        modules={modules}
        isLoading={isLoading}
        isFetchingNextPage={isFetchingNextPage}
        hasNextPage={hasNextPage}
        fetchNextPage={fetchNextPage}
        filtered={search.trim() !== "" || statusFilter !== null || typeFilter !== null}
      />
      <ModuleAdminDialogs />
    </div>
  )
}

import { useMemo } from "react"
import { BlocksIcon } from "lucide-react"
import { useModules, useModuleChangedSubscription } from "@/graphql/hooks/modules"
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

  const search = useModuleStore((s) => s.search)
  const statusFilter = useModuleStore((s) => s.statusFilter)

  const { data, isLoading } = useModules(statusFilter ? [statusFilter] : null)

  // Search is client-side over the (small) list: instance, type, or version.
  const modules = useMemo(() => {
    const all = data?.modules ?? []
    const q = search.trim().toLowerCase()
    if (!q) return all
    return all.filter(
      (m) =>
        m.instance.toLowerCase().includes(q) ||
        m.type.toLowerCase().includes(q) ||
        m.version.toLowerCase().includes(q) ||
        m.description.toLowerCase().includes(q),
    )
  }, [data, search])

  return (
    <div className="flex flex-1 flex-col gap-2 p-2">
      <ModulesToolbar />
      <ModulesTable modules={modules} isLoading={isLoading} />
      <ModuleAdminDialogs />
    </div>
  )
}

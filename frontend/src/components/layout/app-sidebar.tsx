import * as React from "react"

import { NavMain } from "@/components/nav-main"
import { NavUser } from "@/components/nav-user"
import { OperationSwitcher } from "@/components/layout/operation-switcher"
import {
  navigationGlobalItems,
  navigationOperationItems,
  navigationOperationTopItems,
  navigationAdminItems,
} from "@/navigation"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar"
import { warningMark } from "@/components/storage/storage-values"
import { Permissions } from "@/constants/permissions"
import { useStorageWarnings } from "@/graphql/hooks/storage-warnings"
import { useAuthStore } from "@/stores/auth"
import { useScopedOperationStore } from "@/stores/scoped-operation"
import { cn } from "@/lib/utils"

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const user = useAuthStore((s) => s.user)
  const hasPermission = useAuthStore((s) => s.hasPermission)
  const scopedOperation = useScopedOperationStore((s) => s.scopedOperation)

  // Filter admin nav items by the user's permissions
  const visibleAdminItems = navigationAdminItems.filter(
    (item) => !item.permission || hasPermission(item.permission),
  )
  // Storage is marked with the warnings that need an admin, in the worst's
  // colour: writes failing, or about to.
  const storage = useStorageWarnings(hasPermission(Permissions.STORAGE_READ))
  const mark = warningMark(storage.data?.storageStatus.warnings ?? [])
  const adminBadges = mark
    ? {
        "/storage": (
          <span
            className={cn("rounded px-1", mark.level === "error" ? "bg-red-500/15 text-red-700 dark:text-red-300" : "bg-amber-500/15 text-amber-700 dark:text-amber-300")}
            title={`${mark.count} storage ${mark.count === 1 ? "warning" : "warnings"}`}
          >
            {mark.count}
          </span>
        ),
      }
    : undefined

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <OperationSwitcher />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      {/* Global items (Findings) stay visible whether or not an operation
          is scoped; Findings switches to its cross-operation mode in that
          case so users can search across the operations they belong to.
          Render scoped + global items inside a single NavMain (and thus a
          single SidebarGroup) so the spacing between them matches the
          gap between scoped items — otherwise stacked p-2 padding from two
          groups doubles the visual gap. */}
      <SidebarContent className="flex flex-col">
        {/* data-tour anchors the walkthrough step that explains why Tasks and
            Timeline appear only once an operation is scoped. */}
        <div data-tour="nav-operation-items">
          <NavMain
            items={
              scopedOperation
                ? [...navigationOperationTopItems, ...navigationGlobalItems, ...navigationOperationItems]
                : navigationGlobalItems
            }
          />
        </div>
        {visibleAdminItems.length > 0 && (
          <div className="mt-auto">
            <NavMain items={visibleAdminItems} badges={adminBadges} />
          </div>
        )}
      </SidebarContent>
      <SidebarFooter>
        <NavUser
          user={{
            name: user?.username ?? "Unknown",
            email: "",
            avatar: "",
          }}
        />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

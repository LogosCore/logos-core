import type { ReactNode } from "react"
import { Link, useLocation } from "react-router"
import {
  SidebarGroup,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"
import type { NavigationItem } from "@/navigation"
import { preloadPage } from "@/lib/page-loaders"

/** The sidebar's entries; badges, by an entry's url, mark it (Storage's warnings). */
export function NavMain({ items, badges }: { items: NavigationItem[]; badges?: Record<string, ReactNode> }) {
  const { pathname } = useLocation()

  return (
    <SidebarGroup>
      <SidebarMenu className="gap-1">
        {items.map((item) => (
          <SidebarMenuItem key={item.title}>
            <SidebarMenuButton
              tooltip={item.title}
              isActive={
                !item.externalUrl &&
                (pathname === item.url || pathname.startsWith(item.url + "/"))
              }
              render={
                item.externalUrl ? (
                  <a
                    href={item.externalUrl}
                    target="_blank"
                    rel="noreferrer noopener"
                  />
                ) : (
                  // The page's code is fetched as the pointer or focus
                  // reaches its link: the click then opens it at once.
                  <Link to={item.url} onPointerEnter={() => preloadPage(item.url)} onFocus={() => preloadPage(item.url)} />
                )
              }
            >
              {item.icon && <item.icon className="size-4" />}
              <span>{item.title}</span>
            </SidebarMenuButton>
            {badges?.[item.url] && <SidebarMenuBadge>{badges[item.url]}</SidebarMenuBadge>}
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </SidebarGroup>
  )
}

import * as React from "react"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"

// The sidebar on a narrow window: a sheet over the page (sidebar.tsx renders
// it then). A chunk of its own, with the sheet's dialog underneath it, so a
// desktop never loads them.
export default function SidebarMobile({
  open,
  onOpenChange,
  dir,
  side,
  width,
  sheetProps,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  dir?: string
  side: "left" | "right"
  width: string
  sheetProps: React.ComponentProps<"div">
  children: React.ReactNode
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} {...sheetProps}>
      <SheetContent
        dir={dir}
        data-sidebar="sidebar"
        data-slot="sidebar"
        data-mobile="true"
        className="w-(--sidebar-width) bg-sidebar p-0 text-sidebar-foreground [&>button]:hidden"
        style={
          {
            "--sidebar-width": width,
          } as React.CSSProperties
        }
        side={side}
      >
        <SheetHeader className="sr-only">
          <SheetTitle>Sidebar</SheetTitle>
          <SheetDescription>Displays the mobile sidebar.</SheetDescription>
        </SheetHeader>
        <div className="flex h-full w-full flex-col">{children}</div>
      </SheetContent>
    </Sheet>
  )
}

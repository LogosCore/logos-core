import { Outlet } from "react-router"
import { PageBoundary } from "@/components/page-boundary"
import { AppSidebar } from "@/components/layout/app-sidebar"
import {
  SidebarInset,
  SidebarProvider,
} from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useAppStore } from "@/stores/app"
import { useTaskDeepLink } from "@/hooks/use-task-deep-link"
import { useFocusBeacon } from "@/hooks/use-focus-beacon"
import { useResumeRefetch } from "@/hooks/use-resume-refetch"
import {
  useOperationChangedSubscription,
  useOperationMemberChangedSubscription,
} from "@/graphql/hooks/operations"
import { AgentActivityRail } from "@/components/layout/agent-activity-rail"
import { SkillUpdateDialog } from "@/components/keys/skill-update-dialog"
import { CommunitySkillUpdateDialog } from "@/components/skills/community-skill-update-dialog"
import { OnboardingTour } from "@/components/onboarding/onboarding-tour"
import { deferOverlay } from "@/components/layout/deferred-overlay"
import { useTaskStore } from "@/stores/tasks"
import { useTaskPickerStore } from "@/components/tasks/task-picker-store"
import { usePaletteStore } from "@/components/wiki/wiki-palette-store"

// The six overlays below open from a store flag and render null until then, so
// their modules are fetched on first open rather than parsed at first paint.
// See deferred-overlay.tsx for why the flag is read inside the wrapper and why
// a mounted overlay is never unmounted again.
//
// The task dialogs are warmed on idle: they open from a click on a task row and
// are a few kilobytes each, so a chunk fetch at that moment would read as lag.
// The palette and the task picker are not. Both reach DocumentIcon and the two
// icon catalogs behind it — roughly 1 MB of glob thunk tables — which is the
// weight this whole arrangement exists to keep off the critical path. Warming
// them would only move that parse from first paint into the moment the operator
// starts interacting. They open from a keystroke (Cmd+K) or a menu item, where
// one same-origin request against an immutable cache is not felt.
const EditTaskDialog = deferOverlay(
  () => import("@/components/tasks/edit-task-dialog"),
  "EditTaskDialog",
  () => useTaskStore((s) => s.editDialogOpen),
  { warm: true },
)
const DeleteTaskDialog = deferOverlay(
  () => import("@/components/tasks/delete-task-dialog"),
  "DeleteTaskDialog",
  () => useTaskStore((s) => s.deleteDialogOpen),
  { warm: true },
)
const StatusRequiredDialog = deferOverlay(
  () => import("@/components/tasks/status-required-dialog"),
  "StatusRequiredDialog",
  () => useTaskStore((s) => s.pendingStageChange !== null),
)
const ReopenTaskDialog = deferOverlay(
  () => import("@/components/tasks/reopen-task-dialog"),
  "ReopenTaskDialog",
  () => useTaskStore((s) => s.pendingReopen !== null),
)
const WikiCommandPalette = deferOverlay(
  () => import("@/components/wiki/wiki-command-palette"),
  "WikiCommandPalette",
  () => usePaletteStore((s) => s.config !== null),
)
const TaskPickerDialog = deferOverlay(
  () => import("@/components/tasks/task-picker-dialog"),
  "TaskPickerDialog",
  () => useTaskPickerStore((s) => s.open),
)

export function AppLayout() {
  const sidebarOpen = useAppStore((s) => s.sidebarOpen)
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen)

  // The task edit dialog is mounted globally so click-to-open works from any
  // surface that lists tasks (kanban board, matrix, wiki "Task backlinks"
  // footer, credential "Referenced by tasks" panel). Keeping it on the
  // tasks page would force a navigation away from the source context every
  // time an operator drilled into a referenced task.
  //
  // Deep-link sync lives at the same level so `?task=<id>` in the URL opens
  // the dialog on any authed page, not just `/tasks`. The create dialog
  // stays on the tasks page — it needs the page's scoped operation context
  // and there's no cross-domain entry point for it.
  useTaskDeepLink()

  // The attention channel. Publishes what the operator is currently looking
  // at so an AI agent connected over MCP can follow along rather than asking
  // which operation or page to work on. Mounted here because it needs router
  // context (App.tsx nests BrowserRouter inside the providers) and because
  // every authed surface should report, not just one page.
  useFocusBeacon()

  // Subscriptions are dropped while the tab is hidden and nothing replays what
  // they missed, so the caches they feed come back stale. Mounted alongside the
  // beacon because the two describe the same absence from the operator's side:
  // one says they stopped watching, this one catches them up when they return.
  useResumeRefetch()

  // Operation and membership changes, on every authed page. These lived on
  // /operations only, so every other list of operations went stale: the
  // sidebar switcher, and the getting-started panel, which kept telling an
  // operator who had just been added to an operation that they were in none
  // until they reloaded. The server passes "you were added" through even when
  // the subscriber had no operations when it subscribed.
  useOperationChangedSubscription()
  useOperationMemberChangedSubscription()

  return (
    <TooltipProvider>
      <SidebarProvider
        open={sidebarOpen}
        onOpenChange={setSidebarOpen}
        // h-svh locks the wrapper to exactly one viewport. Without this
        // the shadcn primitive uses min-h-svh (a *minimum*), and any
        // tall content (e.g. the kanban column virtualizer's sizer)
        // would push the wrapper past the viewport and trigger a global
        // page scroll instead of the column's internal overflow scroll.
        className="h-svh"
      >
        <AppSidebar />
        <SidebarInset className="min-w-0">
          {/* Inside the shell, so the sidebar stays while a page loads. */}
          <PageBoundary>
            <Outlet />
          </PageBoundary>
          {/* Floated over the page rather than placed in it: every surface
              should show what the agent is doing, and no page owns the
              concern. Renders nothing when no agent is active. */}
          <div className="pointer-events-none fixed bottom-4 right-4 z-40 flex justify-end">
            <div className="pointer-events-auto">
              <AgentActivityRail />
            </div>
          </div>
        </SidebarInset>
        <EditTaskDialog />
        <DeleteTaskDialog />
        <StatusRequiredDialog />
        <ReopenTaskDialog />
        {/* Unified wiki search/picker palette, mounted globally. Drives both
            the Cmd+K / "search within" navigate surface (openWikiSearch) and
            every document-reference picker (openWikiDocumentPicker): the /doc
            slash command, the move dialog's parent chooser, and the task edit
            dialog's "Wiki references". */}
        <WikiCommandPalette />
        {/* Task picker is the mirror image — the wiki editor's "Add to task"
            button calls openTaskPicker imperatively to attach the current
            document to a task without leaving the wiki page. */}
        <TaskPickerDialog />
        {/* Prompts a re-download of the agent skill when the server ships a
            newer release than the operator installed. Global so it can appear
            on any authed surface on first load; silent for anyone who never
            downloaded the skill. */}
        <SkillUpdateDialog />
      <CommunitySkillUpdateDialog />
      {/* First-login walkthrough. Global for the same reason the prompts above
          are: it starts on whichever page the operator landed on and finishes
          on the wiki, so no single page can own it. Renders nothing for anyone
          who has been through it. */}
      <OnboardingTour />
      </SidebarProvider>
    </TooltipProvider>
  )
}

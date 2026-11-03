import { lazy, useEffect, useRef, type ComponentType } from "react"
import { pageLoaders } from "@/lib/page-loaders"
import { createBrowserRouter, Navigate, Route, Routes, useParams, useSearchParams } from "react-router"
import { RouterProvider } from "react-router/dom"
import { QueryProvider } from "@/providers/query-provider"
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/sonner"
import { ProtectedRoute } from "@/components/protected-route"
import { ConnectivityBanner } from "@/components/connectivity-banner"
import { PageBoundary } from "@/components/page-boundary"
import { AppLayout } from "@/components/layout/app-layout"
import { useAuthStore } from "@/stores/auth"
import { useConnectivityStore } from "@/stores/connectivity"
import { RecoveryRecheckGate } from "@/lib/recovery-recheck"
import { searchOf, withRecordModal, type RecordModal } from "@/lib/record-modals"

// Every page is its own chunk, fetched the first time it is visited, so the
// first paint waits for the shell and one page rather than for all of them.
// PageBoundary shows the loading and failure states.
function page<K extends string>(
  load: () => Promise<Record<K, ComponentType>>,
  name: K,
) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}

// Their code is in lib/page-loaders.ts, which the sidebar fetches ahead of a
// visit with.
const LoginPage = page(pageLoaders["/login"], "LoginPage")
const EnrollPage = page(pageLoaders["/enroll"], "EnrollPage")
const OperationsPage = page(pageLoaders["/operations"], "OperationsPage")
const UsersPage = page(pageLoaders["/users"], "UsersPage")
const ModulesPage = page(pageLoaders["/modules"], "ModulesPage")
const ModuleDetailPage = page(pageLoaders["/modules/:instance"], "ModuleDetailPage")
const SkillsPage = page(pageLoaders["/skills"], "SkillsPage")
const WikiPage = page(pageLoaders["/wiki"], "WikiPage")
const WikiPrintPage = page(pageLoaders["/wiki/print"], "WikiPrintPage")
const FindingsPage = page(pageLoaders["/findings"], "FindingsPage")
const TasksPage = page(pageLoaders["/tasks"], "TasksPage")
const EndpointsPage = page(pageLoaders["/endpoints"], "EndpointsPage")
const BuildsPage = page(pageLoaders["/builds"], "BuildsPage")
const MinionSessionsPage = page(pageLoaders["/sessions"], "MinionSessionsPage")
const AgentsPage = page(pageLoaders["/agents"], "AgentsPage")
const TimelinePage = page(pageLoaders["/timeline"], "TimelinePage")

/**
 * Where a build or an endpoint had its own page (/builds/:id,
 * /builds/new?kind=&from=, the same for endpoints): the same record in its
 * modal over the list.
 */
function RecordPageRedirect({ list }: { list: "builds" | "endpoints" }) {
  const { id } = useParams()
  const [query] = useSearchParams()
  const kind = query.get("kind") ?? ""
  const from = query.get("from")
  const modal: RecordModal =
    list === "builds"
      ? id ? { kind: "build", id } : { kind: "new-build", factory: kind, from }
      : id ? { kind: "endpoint", id } : { kind: "new-endpoint", channel: kind, from }
  return <Navigate to={`/${list}${searchOf(withRecordModal(new URLSearchParams(), modal))}`} replace />
}

function AppRoutes() {
  return (
    <PageBoundary fullScreen>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/enroll" element={<EnrollPage />} />
        <Route element={<ProtectedRoute />}>
          <Route element={<AppLayout />}>
            {/* No landing page: Tasks is the working surface and shows a
                "pick an operation" prompt when nothing is scoped. */}
            <Route index element={<Navigate to="/wiki" replace />} />
            <Route path="operations" element={<OperationsPage />} />
            <Route path="users" element={<UsersPage />} />
            <Route path="modules" element={<ModulesPage />} />
            <Route path="modules/:instance" element={<ModuleDetailPage />} />
            <Route path="skills" element={<SkillsPage />} />
            <Route path="wiki" element={<WikiPage />} />
            <Route path="wiki/:documentId" element={<WikiPage />} />
            <Route path="findings" element={<FindingsPage />} />
            <Route path="tasks" element={<TasksPage />} />
            <Route path="endpoints" element={<EndpointsPage />} />
            <Route path="builds" element={<BuildsPage />} />
            <Route path="sessions" element={<MinionSessionsPage />} />
            {/* Builds and endpoints open in modals now (?build=<id>, …);
                their old pages, for anyone's bookmark. */}
            <Route path="endpoints/new" element={<RecordPageRedirect list="endpoints" />} />
            <Route path="endpoints/:id" element={<RecordPageRedirect list="endpoints" />} />
            <Route path="builds/new" element={<RecordPageRedirect list="builds" />} />
            <Route path="builds/:id" element={<RecordPageRedirect list="builds" />} />
            <Route path="timeline" element={<TimelinePage />} />
            {/* One route for both tabs: the page reads the tab off the
                pathname, and a single element means switching tabs
                reconciles rather than remounting, so the filters in the URL
                and the sheet on screen survive the navigation. */}
            <Route path="agents/*" element={<AgentsPage />} />
            {/* Where agent activity used to live, for anyone's bookmark. */}
            <Route
              path="agent-activity"
              element={<Navigate to="/agents/activity" replace />}
            />
          </Route>
          {/* Chromeless print view — sits inside ProtectedRoute so the
              auth guard still applies, but outside AppLayout so it
              renders without the sidebar / top nav. The browser's print
              dialog opens automatically once the document loads; the
              user picks "Save as PDF" from there. */}
          <Route
            path="wiki/:documentId/print"
            element={<WikiPrintPage />}
          />
        </Route>
      </Routes>
    </PageBoundary>
  )
}

// A data router, because only it has useBlocker: a build or endpoint modal
// with unsaved changes asks before a navigation closes it (components/ui/
// record-modal.tsx). The routes stay declared with <Routes>, under a single
// catch-all route, the way React Router's guide moves an app across.
const router = createBrowserRouter([{ path: "*", element: <AppRoutes /> }])

function App() {
  const checkAuth = useAuthStore((s) => s.checkAuth)
  const reachable = useConnectivityStore((s) => s.reachable)
  const wasUnreachable = useRef(false)
  const recheckGate = useRef(new RecoveryRecheckGate())

  // Validate stored token on app load (handles page refresh)
  useEffect(() => {
    checkAuth()
  }, [checkAuth])

  // Retry auth check when backend recovers from an outage.
  // Only fires on false→true transition of reachable (skips initial mount).
  // Rate limited: if checkAuth is itself what flips reachability, re-running
  // it on every recovery loops forever (see lib/recovery-recheck.ts).
  useEffect(() => {
    if (!reachable) {
      wasUnreachable.current = true
    } else if (wasUnreachable.current) {
      wasUnreachable.current = false
      if (recheckGate.current.shouldRecheck(Date.now())) checkAuth()
    }
  }, [reachable, checkAuth])

  return (
    <QueryProvider>
      <ThemeProvider>
        <Toaster />
        <ConnectivityBanner />
        <RouterProvider router={router} />
      </ThemeProvider>
    </QueryProvider>
  )
}

export default App

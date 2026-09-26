import { lazy, useEffect, useRef, type ComponentType } from "react"
import { BrowserRouter, Navigate, Route, Routes } from "react-router"
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

// Every page is its own chunk, fetched the first time it is visited, so the
// first paint waits for the shell and one page rather than for all of them.
// PageBoundary shows the loading and failure states.
function page<K extends string>(
  load: () => Promise<Record<K, ComponentType>>,
  name: K,
) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}

const LoginPage = page(() => import("@/pages/login"), "LoginPage")
const EnrollPage = page(() => import("@/pages/enroll"), "EnrollPage")
const OperationsPage = page(() => import("@/pages/operations"), "OperationsPage")
const UsersPage = page(() => import("@/pages/users"), "UsersPage")
const ModulesPage = page(() => import("@/pages/modules"), "ModulesPage")
const SkillsPage = page(() => import("@/pages/skills"), "SkillsPage")
const WikiPage = page(() => import("@/pages/wiki"), "WikiPage")
const WikiPrintPage = page(() => import("@/pages/wiki-print"), "WikiPrintPage")
const FindingsPage = page(() => import("@/pages/findings"), "FindingsPage")
const TasksPage = page(() => import("@/pages/tasks"), "TasksPage")
const AgentActivityPage = page(
  () => import("@/pages/agent-activity"),
  "AgentActivityPage",
)
const TimelinePage = page(() => import("@/pages/timeline"), "TimelinePage")

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
        <BrowserRouter>
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
              <Route path="skills" element={<SkillsPage />} />
              <Route path="wiki" element={<WikiPage />} />
              <Route path="wiki/:documentId" element={<WikiPage />} />
              <Route path="findings" element={<FindingsPage />} />
              <Route path="tasks" element={<TasksPage />} />
              <Route path="timeline" element={<TimelinePage />} />
              <Route path="agent-activity" element={<AgentActivityPage />} />
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
        </BrowserRouter>
      </ThemeProvider>
    </QueryProvider>
  )
}

export default App

// Every page's code, by its path: what the router lazy-loads on a first
// visit (App.tsx), and what is fetched ahead of it (preloadPage) when a
// visit is likely: the sidebar link hovered or focused, the Sessions page
// once the app is idle. A page fetched ahead of the click opens at once;
// otherwise the router, which navigates in a transition, holds the page
// before on screen while the new one's code loads, which in development
// (one request per module) can take seconds.

export const pageLoaders = {
  "/login": () => import("@/pages/login"),
  "/enroll": () => import("@/pages/enroll"),
  "/operations": () => import("@/pages/operations"),
  "/users": () => import("@/pages/users"),
  "/modules": () => import("@/pages/modules"),
  "/modules/:instance": () => import("@/pages/module-detail"),
  "/skills": () => import("@/pages/skills"),
  "/wiki": () => import("@/pages/wiki"),
  "/wiki/print": () => import("@/pages/wiki-print"),
  "/findings": () => import("@/pages/findings"),
  "/tasks": () => import("@/pages/tasks"),
  "/endpoints": () => import("@/pages/endpoints"),
  "/builds": () => import("@/pages/builds"),
  "/sessions": () => import("@/pages/minion-sessions"),
  "/ports": () => import("@/pages/ports"),
  "/agents": () => import("@/pages/agents"),
  "/timeline": () => import("@/pages/timeline"),
} as const

export type PagePath = keyof typeof pageLoaders

/** The page a path opens: its own, or the one of the section it is in (/wiki/abc → /wiki); null for none. */
export function pageOf(path: string): PagePath | null {
  const clean = path.split(/[?#]/)[0].replace(/\/+$/, "") || "/"
  if (clean in pageLoaders) return clean as PagePath
  const section = "/" + (clean.split("/")[1] ?? "")
  return section in pageLoaders ? (section as PagePath) : null
}

/** Fetches a page's code ahead of a visit; a failure is the visit's to report. */
export function preloadPage(path: string): void {
  const page = pageOf(path)
  if (!page) return
  const load: () => Promise<unknown> = pageLoaders[page]
  void load().catch(() => {})
}

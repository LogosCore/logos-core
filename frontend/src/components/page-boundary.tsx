import { Component, Suspense, type ErrorInfo, type ReactNode } from "react"
import { useLocation } from "react-router"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/**
 * Wraps the lazily loaded pages (see App.tsx). While a page's chunk
 * downloads it shows "Loading...", as the auth guard does. If the chunk
 * fails to load, or the page throws while rendering, it offers a reload in
 * the page's place instead of unmounting the whole app.
 *
 * The usual cause of a failed chunk is a deploy: it replaces the hashed
 * files, and a tab opened before it still asks for the old names. A reload
 * fetches the current index.html, which nginx never caches.
 *
 * `fullScreen` centres the placeholder in the viewport, for pages rendered
 * outside the app shell; otherwise it fills the shell's content area.
 */
export function PageBoundary({
  fullScreen = false,
  children,
}: {
  fullScreen?: boolean
  children: ReactNode
}) {
  const { pathname } = useLocation()
  const frame = cn(
    "flex items-center justify-center text-muted-foreground",
    fullScreen ? "min-h-svh" : "flex-1",
  )
  return (
    <PageErrorBoundary pathname={pathname} className={frame}>
      <Suspense fallback={<div className={frame}>Loading...</div>}>
        {children}
      </Suspense>
    </PageErrorBoundary>
  )
}

interface PageErrorBoundaryProps {
  pathname: string
  className: string
  children: ReactNode
}

interface PageErrorBoundaryState {
  error: Error | null
  pathname: string
}

// Navigating to another page clears the error, so one broken page leaves
// the rest of the app usable.
class PageErrorBoundary extends Component<
  PageErrorBoundaryProps,
  PageErrorBoundaryState
> {
  state: PageErrorBoundaryState = {
    error: null,
    pathname: this.props.pathname,
  }

  static getDerivedStateFromProps(
    props: PageErrorBoundaryProps,
    state: PageErrorBoundaryState,
  ): Partial<PageErrorBoundaryState> | null {
    if (props.pathname !== state.pathname) {
      return { error: null, pathname: props.pathname }
    }
    return null
  }

  static getDerivedStateFromError(error: Error): Partial<PageErrorBoundaryState> {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Page crashed:", error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <div className={cn(this.props.className, "flex-col gap-3")}>
          <p className="text-sm">This page failed to load.</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => window.location.reload()}
          >
            Reload
          </Button>
        </div>
      )
    }
    return this.props.children
  }
}

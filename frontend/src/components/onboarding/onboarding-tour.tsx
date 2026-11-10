// The in-app guides: a spotlight over one control at a time, with a card
// explaining it.
//
// Mounted globally from AppLayout, like the skill-update prompt, because they
// cross surfaces — the welcome guide starts on whatever page the operator landed
// on and ends on the wiki.
//
// Hand-rolled rather than pulled from a tour library: the machines already have
// to observe real app state (an operation becoming scoped, a popover mounting,
// the "/" menu opening), which is the hard half, and what is left is a rectangle
// and a card. A library would add a dependency for the easy half and still need
// this file to drive it.

import { lazy, Suspense, useEffect } from "react"
import { useParams } from "react-router"

import { useAuthStore } from "@/stores/auth"
import { useIsMobile } from "@/hooks/use-mobile"
import { useOnboardingStore } from "@/stores/onboarding"
import { useScopedOperationStore } from "@/stores/scoped-operation"
import { useMe } from "@/graphql/hooks/users"
import { useInfiniteOperations } from "@/graphql/hooks/operations"
import type { GuideID } from "@/components/onboarding/tour-steps"

// The guide on screen loads when a guide starts (tour-overlay.tsx).
const TourOverlay = lazy(() => import("./tour-overlay"))

export function OnboardingTour() {
  const guide = useOnboardingStore((s) => s.guide)

  // The auto-offer decisions and the tour itself are separate components so the
  // measuring loop below only ever runs while a guide is on screen.
  return (
    <>
      <GuideAutoStart />
      {guide ? (
        <Suspense fallback={null}>
          <TourOverlay guide={guide} />
        </Suspense>
      ) : null}
    </>
  )
}

/**
 * Offers each guide once, to an operator who has never been through it.
 *
 * The welcome guide is gated on having somewhere to go: its second step is "pick
 * an operation", so running it for someone who belongs to none would spotlight
 * an empty list. Those operators get the getting-started panel's "ask an admin"
 * branch instead, which is the only honest advice available to them.
 *
 * It is deliberately NOT gated on nothing being scoped. A remembered scope is
 * not evidence of understanding: it is restored from localStorage on login, so
 * an operator who picked an operation on their first day and came back on their
 * second would have the guide suppressed forever, with the server still
 * recording that they had never seen it. `me.completedGuides` is the only thing
 * that decides. Someone already scoped fast-forwards past the two picking steps
 * instead — see reduce().
 *
 * The document guide waits until the operator is actually in a page — which is
 * where creating their first document lands them, and also catches anyone added
 * to an operation that already has pages.
 */
function GuideAutoStart() {
  const isMobile = useIsMobile()
  const { data: me } = useMe()
  const userId = useAuthStore((s) => s.user?.userId)
  const scopedOperation = useScopedOperationStore((s) => s.scopedOperation)
  const running = useOnboardingStore((s) => s.guide)
  const offered = useOnboardingStore((s) => s.offered)
  const start = useOnboardingStore((s) => s.start)
  const dismissedLocally = useOnboardingStore((s) => s.dismissedLocally)

  // Same query variables the switcher and the getting-started panel use, so all
  // three share one cache entry rather than each fetching the list.
  const { data: operations } = useInfiniteOperations({ search: null, first: 20 })
  const operationCount = operations?.pages[0]?.operations.totalCount

  // In a wiki document rather than on the tree-only wiki route.
  const { documentId } = useParams()

  useEffect(() => {
    if (running || isMobile || !userId) return
    // Undefined while `me` is in flight — deciding now would flash a guide at
    // someone who finished it months ago.
    if (!me?.me) return
    const completed = me.me.completedGuides

    const eligible = (guide: GuideID) =>
      !completed.includes(guide) && !offered.includes(guide) && !dismissedLocally(userId, guide)

    // Welcome first: an operator mid-way through it should not have the document
    // guide interrupt, and it runs before any document is open anyway.
    if (eligible("welcome")) {
      // Undefined while the list is in flight; a scope already in hand answers
      // the question without it.
      if (!scopedOperation && operationCount === undefined) return
      if (!scopedOperation && operationCount === 0) return
      start("welcome")
      return
    }
    // A page has to be open, and the welcome guide must be behind them — it
    // finishes by pointing at the tree, which is exactly where they click
    // through to a page.
    if (documentId && scopedOperation && eligible("slash-menu")) {
      start("slash-menu")
    }
  }, [
    running,
    isMobile,
    userId,
    me,
    offered,
    documentId,
    scopedOperation,
    operationCount,
    dismissedLocally,
    start,
  ])

  return null
}

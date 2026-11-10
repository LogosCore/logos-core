// The guide on screen (OnboardingTour starts it): a spotlight over the
// step's control and its card. A chunk of its own, loaded when a guide
// starts, not on every page's first paint.

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import { useLocation, useNavigate } from "react-router"
import { MousePointerClickIcon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { useAppStore } from "@/stores/app"
import { useOnboardingStore } from "@/stores/onboarding"
import { useScopedOperationStore } from "@/stores/scoped-operation"
import { useCompleteGuide } from "@/graphql/hooks/users"
import {
  lastStep,
  stepNumber,
  tourStep,
  tourSteps,
  type GuideID,
} from "@/components/onboarding/tour-steps"
import { placeCard, type Placement, type Rect } from "@/components/onboarding/tour-placement"

/** How often the spotlight re-measures its target. Cheap (one
 *  getBoundingClientRect) and it has to survive the sidebar's open/close
 *  transition, a popover mounting and a route change — none of which emit an
 *  event this component could otherwise hook. */
const MEASURE_INTERVAL_MS = 120

/** Padding around the target inside the spotlight cutout. */
const SPOTLIGHT_PAD = 6

function targetSelector(target: string) {
  return `[data-tour="${target}"]`
}

function rectOf(element: Element): Rect {
  const { top, left, width, height } = element.getBoundingClientRect()
  return { top, left, width, height }
}

export default function TourOverlay({ guide }: { guide: GuideID }) {
  const stepID = useOnboardingStore((s) => s.step)
  const send = useOnboardingStore((s) => s.send)
  const skipGuide = useOnboardingStore((s) => s.skip)
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen)
  const scopedOperation = useScopedOperationStore((s) => s.scopedOperation)
  const completeGuide = useCompleteGuide()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  const cardRef = useRef<HTMLDivElement>(null)
  const [targetRect, setTargetRect] = useState<Rect | null>(null)
  const [placement, setPlacement] = useState<Placement | null>(null)

  const step = stepID ? tourStep(guide, stepID) : null
  const total = tourSteps(guide).length

  // The welcome guide's targets all live in the sidebar, and a closed one would
  // leave the spotlight pointing at nothing. Harmless for the document guide.
  useEffect(() => {
    if (guide === "welcome") setSidebarOpen(true)
  }, [guide, stepID, setSidebarOpen])

  // A step that names a route takes the operator there. Guarded on pathname so
  // it does not fight a manual navigation away mid-step.
  useEffect(() => {
    if (step?.route && pathname !== step.route) {
      navigate(step.route)
    }
    // pathname is deliberately not a dependency: re-running on every navigation
    // would drag the operator back if they chose to leave.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step?.route, navigate])

  // Scope changes satisfy the "pick an operation" step.
  const scopedId = scopedOperation?.id ?? null
  useEffect(() => {
    if (scopedId) send({ type: "scope-set" })
  }, [scopedId, send])

  // Measure the current target, and report any *other* step's target
  // appearing — that is how "open the switcher" and "type /" are confirmed,
  // without lifting open state out of the components that own it.
  const measure = useCallback(() => {
    if (!step) return
    const element = document.querySelector(targetSelector(step.target))
    setTargetRect(element ? rectOf(element) : null)

    if (step.advance.on === "element") {
      const awaited = document.querySelector(targetSelector(step.advance.target))
      if (awaited) send({ type: "element-appeared", target: step.advance.target })
    }
  }, [step, send])

  useEffect(() => {
    // First measurement on the next frame rather than in the effect body: the
    // target may still be mid-transition (the sidebar was just told to open),
    // and measuring synchronously here would both catch it moving and trip the
    // cascading-render rule.
    const frame = window.requestAnimationFrame(measure)
    const timer = window.setInterval(measure, MEASURE_INTERVAL_MS)
    window.addEventListener("resize", measure)
    return () => {
      window.cancelAnimationFrame(frame)
      window.clearInterval(timer)
      window.removeEventListener("resize", measure)
    }
  }, [measure])

  // Placement needs the card's own size, so it runs after layout.
  useLayoutEffect(() => {
    if (!targetRect || !cardRef.current) {
      setPlacement(null)
      return
    }
    setPlacement(
      placeCard(targetRect, rectOf(cardRef.current), {
        width: window.innerWidth,
        height: window.innerHeight,
      }),
    )
  }, [targetRect])

  const record = useCallback(() => {
    // Fire and forget: the mutation is optimistic and a failure only means the
    // guide offers itself again next session, which beats blocking the operator
    // behind an error they cannot act on.
    completeGuide.mutate(guide)
  }, [completeGuide, guide])

  const handleSkip = useCallback(() => {
    skipGuide()
    record()
  }, [skipGuide, record])

  const handleAdvance = useCallback(() => {
    const isLast = stepID === lastStep(guide)
    send({ type: "manual" })
    if (isLast) record()
  }, [stepID, guide, send, record])

  // Escape leaves. A guide with no way out is a trap.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") handleSkip()
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [handleSkip])

  if (!step) return null

  // A step with no button of its own is waiting on the operator, so the
  // spotlight beacons and the card states the action.
  const isWaiting = !step.action

  return (
    // pointer-events-none throughout: the operator has to be able to click the
    // very control being pointed at — and to keep typing into the page, for the
    // "/" step — so a forgiving overlay beats a modal one that blocks anything
    // the measured rect gets slightly wrong.
    <div className="pointer-events-none fixed inset-0 z-50">
      {targetRect ? (
        <>
          <div
            aria-hidden
            className="absolute rounded-lg ring-2 ring-primary transition-all duration-200"
            style={{
              top: targetRect.top - SPOTLIGHT_PAD,
              left: targetRect.left - SPOTLIGHT_PAD,
              width: targetRect.width + SPOTLIGHT_PAD * 2,
              height: targetRect.height + SPOTLIGHT_PAD * 2,
              // The dim is the cutout's own shadow, so the hole needs no mask
              // and stays aligned with the ring as the rect animates.
              boxShadow:
                "0 0 0 9999px color-mix(in oklab, var(--background) 72%, transparent)",
            }}
          />
          {/* Beacon rings, on their own elements so their animated box-shadow
              does not fight the cutout's 9999px dim. Two, offset in time, so
              one is always mid-flight. */}
          {isWaiting
            ? ["animate-guide-beacon", "animate-guide-beacon-delayed"].map((animation) => (
                <div
                  key={animation}
                  aria-hidden
                  className={cn("absolute rounded-lg", animation)}
                  style={{
                    top: targetRect.top - SPOTLIGHT_PAD,
                    left: targetRect.left - SPOTLIGHT_PAD,
                    width: targetRect.width + SPOTLIGHT_PAD * 2,
                    height: targetRect.height + SPOTLIGHT_PAD * 2,
                  }}
                />
              ))
            : null}
        </>
      ) : null}

      <div
        ref={cardRef}
        role="dialog"
        aria-labelledby="tour-title"
        aria-describedby="tour-body"
        className={cn(
          "pointer-events-auto absolute w-[min(22rem,calc(100vw-1.5rem))] rounded-xl bg-popover p-4 text-popover-foreground shadow-lg ring-1 ring-foreground/10 transition-all duration-200",
          // Until the first measurement lands the card would otherwise flash at
          // the viewport origin.
          placement ? "opacity-100" : "opacity-0",
        )}
        style={placement ? { top: placement.top, left: placement.left } : { top: 0, left: 0 }}
      >
        <div className="flex items-start justify-between gap-3">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            Step {stepNumber(guide, step.id)} of {total}
          </p>
          <button
            type="button"
            onClick={handleSkip}
            aria-label="Skip this guide"
            className="-mr-1 -mt-1 flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <XIcon className="size-3.5" />
          </button>
        </div>

        <h2 id="tour-title" className="mt-1.5 text-base font-semibold tracking-tight">
          {step.title}
        </h2>
        <p id="tour-body" className="mt-1 text-sm text-muted-foreground">
          {step.body}
        </p>

        {/* The instruction, for a step with no button. Given the weight of a
            call to action rather than a muted aside — this is the one line the
            operator has to read to get any further. */}
        {step.cta ? (
          <p
            aria-live="polite"
            className="mt-3 flex items-start gap-2 rounded-lg bg-primary/10 px-3 py-2 text-sm font-medium text-foreground ring-1 ring-primary/30"
          >
            <MousePointerClickIcon className="mt-0.5 size-4 shrink-0 text-primary" />
            {step.cta}
          </p>
        ) : null}

        <div className="mt-4 flex items-center justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={handleSkip}>
            Skip
          </Button>
          {step.action ? (
            <Button size="sm" onClick={handleAdvance}>
              {step.action}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

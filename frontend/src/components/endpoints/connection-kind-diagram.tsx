import { useEffect, useRef } from "react"
import { cn } from "@/lib/utils"
import { HOP, hops, stepAt, type KindFlow, type LinkState } from "./connection-kind-flows"

// A connection kind's flow, played: the minion, the channel and core in a
// row, the links between them (open, or dashed while nothing is), and the
// messages moving along them; core's pushes in green. It loops while
// mounted, drawing with refs (no render per frame), and tells onStep when
// the step it plays changes. With reduced motion it holds the loop's last
// moment, links as they are while it runs, and says no step.

const W = 360
const H = 120
const Y = 66
const X = { m: 44, c: 180, k: 316 } as const

export function ConnectionKindDiagram({
  flow,
  kind,
  onStep,
  className,
}: {
  flow: KindFlow
  kind: string
  onStep?: (step: number | null) => void
  className?: string
}) {
  const mc = useRef<SVGLineElement>(null)
  const ck = useRef<SVGLineElement>(null)
  const dot = useRef<SVGCircleElement>(null)
  const label = useRef<SVGTextElement>(null)
  const badge = useRef<SVGGElement>(null)
  const badgeText = useRef<SVGTextElement>(null)
  const mark = useRef<SVGTextElement>(null)
  const stepRef = useRef(onStep)
  useEffect(() => {
    stepRef.current = onStep
  }, [onStep])

  useEffect(() => {
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    const styleLink = (line: SVGLineElement | null, state: LinkState) => {
      if (!line) return
      line.setAttribute("stroke", state === "open" ? "var(--foreground)" : "var(--muted-foreground)")
      line.setAttribute("stroke-width", state === "none" ? "1.5" : "2")
      if (state === "none") line.setAttribute("stroke-dasharray", "3 4")
      else line.removeAttribute("stroke-dasharray")
      line.setAttribute("opacity", state === "none" ? "0.6" : "1")
    }
    const draw = (t: number) => {
      const links = flow.links(t, flow.events)
      styleLink(mc.current, links.mc)
      styleLink(ck.current, links.ck)
      badge.current?.setAttribute("opacity", "0")
      mark.current?.setAttribute("opacity", "0")
      let shown = false
      for (const e of flow.events) {
        if (e.badge && t >= e.at && t <= (e.until ?? e.at)) {
          if (badgeText.current) badgeText.current.textContent = e.badge
          badge.current?.setAttribute("opacity", "1")
        }
        if (e.mark && t >= e.at && t <= e.at + 1.4) {
          if (mark.current) mark.current.textContent = e.mark
          mark.current?.setAttribute("opacity", "1")
        }
        if (!e.path) continue
        const hs = hops(e.path)
        const dur = hs.length * HOP
        if (t < e.at || t > e.at + dur) continue
        const p = (t - e.at) / dur
        const i = Math.min(hs.length - 1, Math.floor(p * hs.length))
        const q = p * hs.length - i
        const a = X[hs[i][0] as keyof typeof X]
        const b = X[hs[i][1] as keyof typeof X]
        const x = a + (b - a) * q
        const color = e.push ? "var(--kind-push)" : "var(--foreground)"
        const d = dot.current
        const l = label.current
        if (d && l) {
          d.setAttribute("cx", String(x))
          d.setAttribute("fill", color)
          d.setAttribute("r", e.tiny ? "3" : "5")
          d.setAttribute("opacity", "1")
          l.textContent = e.label ?? ""
          l.setAttribute("x", String(Math.max(44, Math.min(W - 44, x))))
          l.setAttribute("fill", color)
          l.setAttribute("opacity", "1")
        }
        shown = true
      }
      if (!shown) {
        dot.current?.setAttribute("opacity", "0")
        label.current?.setAttribute("opacity", "0")
      }
    }

    if (reduced) {
      // Still: the links as they are while it runs, no message moving.
      draw(flow.period * 0.6)
      dot.current?.setAttribute("opacity", "0")
      label.current?.setAttribute("opacity", "0")
      stepRef.current?.(null)
      return
    }
    let t = 0
    let last = performance.now()
    let step = -1
    let raf = 0
    const frame = (now: number) => {
      t = (t + Math.min(0.1, (now - last) / 1000)) % flow.period
      last = now
      draw(t)
      const s = stepAt(flow, t)
      if (s !== step) {
        step = s
        stepRef.current?.(s)
      }
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame((now) => {
      last = now
      frame(now)
    })
    return () => cancelAnimationFrame(raf)
  }, [flow])

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      role="img"
      aria-label={`${kind}: how messages move between the minion, the channel and core`}
      className={cn("h-auto w-full [--kind-push:var(--color-emerald-600)] dark:[--kind-push:var(--color-emerald-400)]", className)}
    >
      <line ref={mc} x1={X.m + 30} y1={Y} x2={X.c - 30} y2={Y} />
      <line ref={ck} x1={X.c + 30} y1={Y} x2={X.k - 30} y2={Y} />
      {(["m", "c", "k"] as const).map((n) => (
        <g key={n}>
          <rect x={X[n] - 30} y={Y - 15} width={60} height={30} rx={7} fill="var(--card)" stroke="var(--border)" strokeWidth={1.5} />
          <text x={X[n]} y={Y + 4} textAnchor="middle" fontSize={11} fill="var(--foreground)">
            {n === "m" ? "Minion" : n === "c" ? "Channel" : "Core"}
          </text>
        </g>
      ))}
      <g ref={badge} opacity={0}>
        <rect x={X.k - 40} y={Y + 21} width={80} height={18} rx={9} fill="var(--foreground)" />
        <text ref={badgeText} x={X.k} y={Y + 33.5} textAnchor="middle" fontSize={10} fill="var(--background)" />
      </g>
      <text ref={mark} x={(X.m + X.c) / 2} y={Y + 31} textAnchor="middle" fontSize={10} fill="var(--muted-foreground)" opacity={0} />
      <circle ref={dot} r={5} cx={X.m} cy={Y} opacity={0} />
      <text ref={label} x={0} y={Y - 23} textAnchor="middle" fontSize={10.5} className="font-mono" opacity={0} />
    </svg>
  )
}

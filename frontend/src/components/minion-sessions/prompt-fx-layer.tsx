import { useEffect, useRef, useState, type ReactNode } from "react"
import { lightning, type FxEngine } from "./prompt-fx-engine"
import { inferno } from "./prompt-fx-inferno"
import { PromptFxContext, promptFxHandle, useReducedMotion } from "./prompt-fx"
import type { PromptFxName } from "./prompt-fx-model"

/** Each effect's engine, on a canvas. */
const ENGINES: Record<PromptFxName, (canvas: HTMLCanvasElement) => FxEngine> = { lightning, inferno }

/**
 * A console's prompt effect (its operator's /fx): a canvas over the console,
 * clicks passing through it, drawn on as the prompt says what happens
 * (prompt-fx.ts). None while the effect is off or the system asks for less
 * motion. Its parent is the console, positioned: the canvas covers it.
 */
export function PromptFxLayer({ effect, children }: { effect: PromptFxName | null; children: ReactNode }) {
  const [handle] = useState(promptFxHandle)
  const reduced = useReducedMotion()
  const on = effect !== null && !reduced
  const canvas = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    const el = canvas.current
    if (!on || !el || !effect) return
    const engine = ENGINES[effect](el)
    handle.attach(engine)
    return () => {
      handle.attach(null)
      engine.dispose()
    }
  }, [on, effect, handle])
  return (
    <PromptFxContext value={handle}>
      {children}
      {on && <canvas ref={canvas} aria-hidden className="pointer-events-none absolute inset-0 z-10 size-full" />}
    </PromptFxContext>
  )
}

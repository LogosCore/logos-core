import type { MinionTaskFieldsFragment as Task } from "@/graphql/gql/graphql"

// A factory command's task (docs: contracts/factory-commands): its factory
// runs it itself, and nothing goes to the minion. Core offers it to the
// factory's instances, the fewest-loaded first, every 2 seconds until one
// takes it; it is FACTORY from its queueing to its end. What the console
// says of one.

export type FactoryRun = NonNullable<Task["factory"]>

/** The state a factory command's task is in until it ends. */
export const FACTORY = "FACTORY"

type Of = Pick<Task, "state" | "factory">

/** Where a factory command's task stands while it runs. */
export type FactoryStand =
  /** No instance took it yet: why, core's reason at its latest offer (null before one). */
  | { phase: "waiting"; why: string | null; module: string }
  /** Core's call is out to the instance. */
  | { phase: "offered"; instance: string }
  /** The instance took it and runs it. */
  | { phase: "running"; instance: string }

/** Where a factory command's task stands; null once it ended, and for another task. */
export function factoryStands(t: Of): FactoryStand | null {
  const f = t.factory
  if (!f || t.state !== FACTORY) return null
  if (f.instance) return f.acceptedAt ? { phase: "running", instance: f.instance } : { phase: "offered", instance: f.instance }
  return { phase: "waiting", why: f.waiting?.trim() || null, module: f.module }
}

/**
 * Why no instance took a factory command's task, when core said (no
 * instance runs the command, each one is busy): the task waits as a held
 * one does, in amber. Null otherwise.
 */
export function factoryHeld(t: Of): string | null {
  const s = factoryStands(t)
  return s?.phase === "waiting" ? s.why : null
}

/** The factory instance that ran a factory command's task, once it ended; null for one none took, and for another task. */
export function ranOn(t: Of): string | null {
  const f = t.factory
  if (!f || t.state === FACTORY || !f.acceptedAt) return null
  return f.instance ?? null
}

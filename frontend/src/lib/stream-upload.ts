import { useSyncExternalStore } from "react"
import { apiFetch } from "@/services/api-client"

// A file this browser streams to a minion (docs:
// contracts/task-results-and-actions.md, Uploading a stream): sent to
// PUT /minion-streams/{task} in parts, one after the other, each from where
// core says the stream is, so a failure goes on from there. Core takes a
// part as the minion takes it, so a part may wait long: the minion's next
// check-in, or a slow link. The uploads live for the page, not a view:
// switching tabs in the app leaves them going.

/** One part: core reads it as the minion takes it. */
const PART = 8 << 20
/** Tries in a row before an upload stops, waiting longer each time. */
const TRIES = 6
/** A part may wait for the minion's next check-in: a day at most. */
const PART_TIMEOUT = 24 * 60 * 60 * 1000

export interface StreamUpload {
  taskId: string
  name: string
  size: number
  /** Where core has the stream: what it took from this browser. */
  sent: number
  state: "uploading" | "retrying" | "done" | "failed" | "stopped"
  error?: string
}

const uploads = new Map<string, StreamUpload>()
const aborts = new Map<string, AbortController>()
const listeners = new Set<() => void>()

function set(u: StreamUpload) {
  uploads.set(u.taskId, u)
  for (const l of listeners) l()
}

function path(taskId: string) {
  return `/minion-streams/${encodeURIComponent(taskId)}`
}

/** Where core has a stream: its Upload-Offset. */
async function offsetOf(taskId: string, signal: AbortSignal): Promise<number> {
  const res = await apiFetch(path(taskId), { method: "HEAD", signal })
  if (res.status === 410) throw new Error("The stream is over: its task ended.")
  if (!res.ok) throw new Error(`Core answered ${res.status}.`)
  return Number(res.headers.get("Upload-Offset") ?? "0")
}

/**
 * Streams file to a task's minion, from where core has it: a new upload, or
 * one that goes on after a failure or a reload (the same file picked again).
 * An upload already going for the task is stopped first.
 */
export function startStreamUpload(taskId: string, file: File): void {
  aborts.get(taskId)?.abort()
  const abort = new AbortController()
  aborts.set(taskId, abort)
  const base: StreamUpload = { taskId, name: file.name, size: file.size, sent: 0, state: "uploading" }
  set(base)
  void run(base, file, abort.signal)
}

async function run(u: StreamUpload, file: File, signal: AbortSignal) {
  let tries = 0
  let at = 0
  for (;;) {
    try {
      at = await offsetOf(u.taskId, signal)
      set({ ...u, sent: at, state: "uploading", error: undefined })
      if (file.size === 0) {
        const res = await apiFetch(path(u.taskId), { method: "PUT", body: file, signal, timeoutMs: PART_TIMEOUT })
        if (!res.ok && res.status !== 409) throw new Error(`Core answered ${res.status}.`)
      }
      while (at < file.size) {
        const end = Math.min(at + PART, file.size)
        const res = await apiFetch(path(u.taskId), {
          method: "PUT",
          body: file.slice(at, end),
          headers: { "Content-Range": `bytes ${at}-${end - 1}/${file.size}`, "Content-Type": "application/octet-stream" },
          signal,
          timeoutMs: PART_TIMEOUT,
        })
        if (res.status === 410) throw new Error("The stream is over: its task ended.")
        if (res.status === 400 || res.status === 411) {
          const body = (await res.json().catch(() => null)) as { error?: string } | null
          set({ ...u, sent: at, state: "failed", error: body?.error ?? `Core answered ${res.status}.` })
          return
        }
        if (res.status !== 204 && res.status !== 409) throw new Error(`Core answered ${res.status}.`)
        at = Number(res.headers.get("Upload-Offset") ?? end)
        tries = 0
        set({ ...u, sent: at, state: "uploading", error: undefined })
      }
      set({ ...u, sent: file.size, state: "done" })
      return
    } catch (err) {
      if (signal.aborted) {
        set({ ...u, sent: at, state: "stopped" })
        return
      }
      const message = err instanceof Error ? err.message : String(err)
      if (message.startsWith("The stream is over") || ++tries >= TRIES) {
        set({ ...u, sent: at, state: "failed", error: message })
        return
      }
      set({ ...u, sent: at, state: "retrying", error: message })
      await new Promise((r) => setTimeout(r, 1000 * 2 ** tries))
    }
  }
}

/** Stops a task's upload: the stream keeps what core has, for a resume. */
export function stopStreamUpload(taskId: string): void {
  aborts.get(taskId)?.abort()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** A task's upload from this page, as it goes; undefined for none. */
export function useStreamUpload(taskId: string): StreamUpload | undefined {
  return useSyncExternalStore(subscribe, () => uploads.get(taskId))
}

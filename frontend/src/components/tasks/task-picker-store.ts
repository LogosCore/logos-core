// The task picker's state and imperative entry point, split out of
// task-picker-dialog.tsx for the same bundling reason as
// wiki-palette-store.ts: `openTaskPicker` is called from the wiki editor, and
// while it lived beside the dialog that import dragged the dialog's virtualizer
// and task-list machinery along with it. AppLayout mounts the dialog, so it sat
// in the shell's static graph and first paint parsed it on every page.
//
// Keep this module free of UI imports. task-picker-dialog.tsx is loaded by the
// deferred mount in app-layout.tsx, the first time the picker opens.

import { create } from "zustand"
import type { TaskFieldsFragment } from "@/graphql/gql/graphql"

// Shape passed back to the caller on pick. Matches the subset of TaskFields
// every consuming surface needs — the wiki "Add to task" trigger uses
// `name` for the toast, the rest is there for completeness in case future
// callers want to render extra context post-pick.
export interface PickedTask {
  id: string
  name: string
  stage: TaskFieldsFragment["stage"]
  status: TaskFieldsFragment["status"]
}

export interface OpenArgs {
  operationId: string
  /** Task IDs that should appear muted and reject clicks (e.g. tasks that
   *  already reference the current wiki document). */
  excludeIds?: string[]
  /** Optional override for the dialog title — defaults to "Link to a task". */
  title?: string
  /** Optional override for the dialog description. */
  description?: string
  onPick: (task: PickedTask) => void
}

interface PickerState {
  open: boolean
  operationId: string
  excludeIds: string[]
  title: string
  description: string
  onPick: ((task: PickedTask) => void) | null
  openPicker: (args: OpenArgs) => void
  closePicker: () => void
}

// Singleton store — same pattern as the wiki document picker. Any surface
// that needs to pick a task (today: the wiki editor's "Add to task"
// button; later potentially the timeline / matrix views) calls
// `openTaskPicker` to trigger it.
export const useTaskPickerStore = create<PickerState>((set) => ({
  open: false,
  operationId: "",
  excludeIds: [],
  title: "Link to a task",
  description: "Pick a task in this operation.",
  onPick: null,
  openPicker: ({ operationId, excludeIds, title, description, onPick }) =>
    set({
      open: true,
      operationId,
      excludeIds: excludeIds ?? [],
      title: title ?? "Link to a task",
      description: description ?? "Pick a task in this operation.",
      onPick,
    }),
  closePicker: () =>
    set({
      open: false,
      operationId: "",
      excludeIds: [],
      onPick: null,
    }),
}))

/** Imperative entry point — same shape regardless of which surface opens it. */
export function openTaskPicker(args: OpenArgs) {
  useTaskPickerStore.getState().openPicker(args)
}

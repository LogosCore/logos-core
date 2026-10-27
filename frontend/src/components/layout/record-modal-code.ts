import { preloadable } from "@/lib/preloadable"

// The build and endpoint modals' code (record-modals.tsx). None of it is in
// the shell: it is fetched in the background once a page has settled, so the
// first modal opened shows at once, or on opening one before that.
export const buildModalCode = preloadable(() => import("@/components/builds/build-modal"))
export const endpointModalCode = preloadable(() => import("@/components/endpoints/endpoint-modal"))
// The new-build form, on its own: most of the build modal's code, and a
// build's details do not use it.
export const buildEditorCode = preloadable(() => import("@/components/builds/build-editor"))

/** Fetches all of it. A failure stays quiet here; opening a modal reports it. */
export function preloadRecordModals() {
  for (const code of [buildModalCode, endpointModalCode, buildEditorCode]) code.load().catch(() => {})
}

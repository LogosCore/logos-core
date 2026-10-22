// Putting elements on the canvas where the operator is looking: the Shapes
// panel's click-to-insert, and record cards placed from a picker or a paste.
// The id and position arithmetic is drawing-insert.ts; this is the part that
// needs the canvas.

import { CaptureUpdateAction, convertToExcalidrawElements } from "@excalidraw/excalidraw"
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import type { ExcalidrawElementSkeleton } from "@excalidraw/excalidraw/data/transform"

import type { Skeleton } from "./drawing-catalogue"
import { centeredAt, viewportCenter, withFreshIds } from "./drawing-insert"

export const SHAPES_SIDEBAR = "logos-shapes"

/** Catalogue skeletons as canvas elements. Built after the canvas has loaded
 * its fonts, so text is measured in the font it is drawn in. */
export function skeletonElements(skeletons: readonly Skeleton[]): ExcalidrawElement[] {
  // The catalogue builds Excalidraw skeletons without importing Excalidraw
  // (it is shared with the sidecar), so its type is a loose version of this.
  return convertToExcalidrawElements(skeletons as unknown as ExcalidrawElementSkeleton[], { regenerateIds: true })
}

/** Insert `elements` centred on `center` (by default the view's), selected
 * and undoable. */
export function insertElements(
  api: ExcalidrawImperativeAPI,
  elements: readonly ExcalidrawElement[],
  center: { x: number; y: number } = viewportCenter(api.getAppState()),
) {
  const placed = centeredAt(withFreshIds(elements), center)
  // Selected the way a click on it would select it: the outermost group, and
  // every element except labels, which Excalidraw selects through their
  // container.
  const groups = new Set(placed.map((el) => el.groupIds.at(-1)).filter((g): g is string => !!g))
  const selected = placed.filter((el) => !(el as { containerId?: string | null }).containerId)
  api.updateScene({
    elements: [...api.getSceneElementsIncludingDeleted(), ...placed],
    appState: {
      selectedElementIds: Object.fromEntries(selected.map((el) => [el.id, true])),
      selectedGroupIds: Object.fromEntries([...groups].map((g) => [g, true])),
      editingGroupId: null,
    } as never,
    captureUpdate: CaptureUpdateAction.IMMEDIATELY,
  })
  // On a narrow screen the panel covers the canvas; get out of the way of
  // the shape that was just put there.
  if (window.matchMedia("(max-width: 767px)").matches) api.toggleSidebar({ name: SHAPES_SIDEBAR, force: false })
}

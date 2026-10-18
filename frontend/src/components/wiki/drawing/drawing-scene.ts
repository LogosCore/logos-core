// The shared representation of a drawing page's scene, and the rules for
// merging one canvas into another.
//
// Deliberately free of React, of the Hocuspocus provider, and of Excalidraw
// itself (its types are imported, which erases at build time — importing the
// library here would drag its chunk into every page that touches a Y.Doc).
// What is left is a pure function of a Y.Doc, which is what makes it testable
// without a browser and reusable by a future inline-diagram node that has no
// page of its own.
//
// Why a Y.Map keyed by element id, rather than a Y.Array of elements or a
// Y.Map per field: Excalidraw already versions its elements individually with
// `version`/`versionNonce` and reconciles at exactly that granularity. Matching
// it means concurrent edits to two different shapes never contend, and two
// edits to the *same* shape resolve the way Excalidraw itself would resolve
// them. A Y.Array would make every insertion a positional conflict for no gain,
// since scene order is carried by the elements themselves.
//
// Why the elements sit at a *root* key rather than nested inside a scene map:
// a nested Y.Map has to be installed by whichever client touches the scene
// first, and two clients opening a blank drawing at the same moment each
// install their own. Yjs resolves that like any other conflicting value — one
// map wins outright — and every element written into the loser is gone, with
// both canvases agreeing on the survivor so nothing ever looks wrong. Root
// keys have no such race: `ydoc.getMap(name)` resolves to the same type for
// every client by construction. The shared appState lives at its own root key
// for the same reason.

import { Map as YMap } from "yjs"
import type { Doc as YDoc } from "yjs"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"

/** An element's layer: our own integer, absent on anything Excalidraw made. */
function layerOf(el: ExcalidrawElement): number {
  const z = (el as { z?: unknown }).z
  return typeof z === "number" && Number.isFinite(z) ? z : 0
}

/** The Y.Doc root key holding the scene. Tiptap owns "default"; this is ours. */
export const DRAWING_ROOT_KEY = "excalidraw"

/**
 * Transaction origin stamped on every write this client makes.
 *
 * Without it the observer would hear its own writes back and push them into
 * the canvas as if they were remote, which at best wastes a render and at
 * worst fights the user's pointer mid-drag.
 */
export const LOCAL_ORIGIN = "drawing-local"

/** Root key holding the shared, page-level slice of Excalidraw's appState.
 * Separate root rather than a field inside the element map: they change at
 * completely different rates and nothing should have to diff one to find the
 * other. */
export const DRAWING_APP_STATE_KEY = "excalidraw:appState"

const BACKGROUND_FIELD = "viewBackgroundColor"
const GRID_FIELD = "gridSize"

/** The subset of Excalidraw's appState that belongs to the page rather than
 * to the person looking at it. Scroll, zoom, selection and the active tool are
 * deliberately absent: sharing those would drag every collaborator's viewport
 * around and make two people unable to look at different parts of one diagram. */
export interface SharedAppState {
  viewBackgroundColor?: string
  gridSize?: number | null
}

/**
 * The element store: element id to element.
 *
 * Idempotent and race-free — every client that asks for this root key gets the
 * same map, with no "who created it" question to resolve.
 */
export function getElementsMap(ydoc: YDoc): YMap<ExcalidrawElement> {
  return ydoc.getMap<ExcalidrawElement>(DRAWING_ROOT_KEY)
}

/** The shared appState store. */
export function getAppStateMap(ydoc: YDoc): YMap<unknown> {
  return ydoc.getMap(DRAWING_APP_STATE_KEY)
}

/**
 * Whether this room already holds a drawing.
 *
 * Reads the shared state rather than the document's `kind`, because the
 * callers that need it most are the ones guarding against the kind having been
 * routed wrongly in the first place. Cheap: root types are resolved by name,
 * and asking for an absent one registers an empty map without emitting an
 * update.
 */
export function isDrawingRoom(ydoc: YDoc): boolean {
  return getElementsMap(ydoc).size > 0
}

/**
 * The order shapes are painted in, and therefore what covers what.
 *
 * Excalidraw takes a flat array and paints it back to front, so the array IS
 * the z-order. Our elements live in a Y.Map keyed by id, which has no order —
 * and handing back its iteration order was a real bug rather than an
 * approximation: Yjs orders that map by CRDT structure, so two people in the
 * same room saw different arrays for identical state. An arrow could be over
 * the box for one of them and under it for the other, with nothing looking
 * wrong to either.
 *
 * The key, derived identically by every client:
 *
 *   1. `z`, our own integer layer. Absent means 0, which is where everything a
 *      person draws sits, so an agent can put something behind the whole
 *      drawing with -1 without knowing what is already there.
 *   2. `index`, Excalidraw's fractional key, within a layer — this is what
 *      preserves the order of shapes a person drew and rearranged in the app.
 *   3. `id`, so the sort is total.
 *
 * Mirrored in hocuspocus/src/drawing-order.ts. The two must agree exactly: a
 * canvas that sorts differently from the server is the same bug in a new place.
 */
function compareElements(a: ExcalidrawElement, b: ExcalidrawElement): number {
  // `z` is ours, not Excalidraw's, so it is read off the element rather than
  // declared on its type — the library has no idea the field exists and must
  // not: carrying it is the only thing we need from it.
  const az = layerOf(a)
  const bz = layerOf(b)
  if (az !== bz) return az - bz

  // Fractional indices compare as plain strings; that is the scheme's whole
  // point. One that is absent sorts first, so an agent-written shape sits
  // behind a person's within the same layer until given a layer of its own.
  const ai = typeof a.index === "string" ? a.index : ""
  const bi = typeof b.index === "string" ? b.index : ""
  if (ai !== bi) return ai < bi ? -1 : 1

  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
}

/** Every element in the shared scene in paint order, including deleted
 * tombstones — Excalidraw expects to receive those and filters them itself. */
export function readElements(ydoc: YDoc): ExcalidrawElement[] {
  return [...getElementsMap(ydoc).values()].sort(compareElements)
}

export function readSharedAppState(ydoc: YDoc): SharedAppState {
  const stored = getAppStateMap(ydoc)
  const state: SharedAppState = {}
  const background = stored.get(BACKGROUND_FIELD)
  if (typeof background === "string") state.viewBackgroundColor = background
  if (stored.has(GRID_FIELD)) state.gridSize = stored.get(GRID_FIELD) as number | null
  return state
}

/**
 * The ids of elements this client is in the middle of editing.
 *
 * An element under an active gesture must never be swapped for a copy that
 * arrived from the room, however that copy compares by version. Excalidraw
 * drives a gesture by mutating one element object *in place* — the freehand
 * stroke growing under the pointer is `appState.newElement`, and every
 * pointermove calls `mutateElement` on that exact object. The scene renders
 * from its own array, so replacing the object there leaves the gesture
 * mutating something the canvas no longer draws: the stroke stops dead at the
 * point the swap happened and is committed truncated on pointerup.
 *
 * The same holds for a resize, a multi-point line being clicked out, a text
 * element being typed into, and a line whose points are being dragged.
 *
 * Mirrors `shouldDiscardRemoteElement` in Excalidraw's own collab layer
 * (data/reconcile.ts), which guards the same appState slots.
 */
export function locallyEditingIDs(appState: LocalEditingAppState): Set<string> {
  const ids = new Set<string>()
  for (const el of [
    appState.newElement,
    appState.resizingElement,
    appState.multiElement,
    appState.editingTextElement,
  ]) {
    if (el?.id) ids.add(el.id)
  }
  const linear = appState.editingLinearElement?.elementId
  if (linear) ids.add(linear)
  return ids
}

/** The slice of Excalidraw's appState that says what is under the pointer.
 * Structural rather than an import of `AppState`, so the merge rules stay
 * testable without conjuring a whole appState. */
export interface LocalEditingAppState {
  newElement?: { id: string } | null
  resizingElement?: { id: string } | null
  multiElement?: { id: string } | null
  editingTextElement?: { id: string } | null
  editingLinearElement?: { elementId: string } | null
}

/**
 * Excalidraw's own conflict rule, applied to one element.
 *
 * Returns true when the local copy should be kept and the remote one dropped:
 * an element under an active local gesture always wins, then a higher version,
 * and an equal version is broken by the lower nonce. The nonce tiebreak is
 * arbitrary but it must be *consistent* — every client has to reach the same
 * answer independently, or two canvases settle into different states and
 * neither ever learns it.
 *
 * An *equal* nonce keeps local too, and that is not a tiebreak at all: two
 * independently-authored edits never collide on a 32-bit random nonce at the
 * same version, so equality means the remote copy IS this client's own last
 * write, read back out of the shared map. The two are the same element with
 * the same content, and the only thing taking the remote one changes is object
 * identity — which is exactly what a gesture in flight cannot survive, per
 * locallyEditingIDs above. Preferring local here costs nothing (the values are
 * identical, so every client still converges) and removes the whole class of
 * self-echo swaps.
 */
export function shouldKeepLocal(
  local: ExcalidrawElement | undefined,
  remote: ExcalidrawElement,
  locallyEditing?: ReadonlySet<string>,
): boolean {
  if (!local) return false
  if (locallyEditing?.has(local.id)) return true
  if (local.version > remote.version) return true
  return local.version === remote.version && local.versionNonce <= remote.versionNonce
}

export interface MergeResult {
  /** The scene to hand to Excalidraw, in paint order. */
  elements: ExcalidrawElement[]
  /**
   * Element id to version, for the elements actually taken from the shared
   * scene.
   *
   * This is what the caller folds into `lastSeen`, and it deliberately omits
   * every element where the local copy won. Recording those too would tell
   * `writeLocalElements` that a version this client has never published is
   * already out there, and it would skip it — so the last change of a gesture
   * that happened to coincide with somebody else's update would stay on the
   * one screen it was drawn on, forever.
   */
  accepted: Map<string, number>
  /**
   * Ids where this canvas kept its own copy and the room holds a different
   * one, with no gesture involved.
   *
   * The room is a Y.Map, and Y.Map settles two concurrent writes to one key by
   * client id — not by Excalidraw's version rule. So when two people change the
   * same shape at the same moment, the room can end up holding the copy the
   * version rule says should lose. Every canvas that holds the rule's winner
   * then keeps it, and every other canvas takes the room's: two screens show
   * different shapes and stay that way until somebody edits it again.
   *
   * Republishing the local copy fixes that. It puts the rule's winner in the
   * room, every canvas that holds the loser takes it by the same rule, and
   * nothing republishes after that, because the room no longer differs from
   * anyone's copy.
   */
  contested: Set<string>
  /**
   * Ids kept only because they are under this client's pointer, where the
   * room's copy would otherwise have won. A gesture must not be interrupted,
   * so the room's copy is dropped. When the gesture ends, the result has to
   * outrank the dropped copy or it never shows on any other screen — see
   * rebaseFinishedEdits.
   */
  deferred: Set<string>
}

/** Whether two copies of an element are the same write. Versions and nonces
 * are what every merge decision reads, so two copies that match on both are
 * interchangeable for convergence. */
function sameWrite(a: ExcalidrawElement, b: ExcalidrawElement): boolean {
  return a.version === b.version && a.versionNonce === b.versionNonce
}

/**
 * Merge the shared scene into the canvas's current elements.
 *
 * The result is returned in paint order, which is what Excalidraw renders
 * from. Every client computes that order from the elements themselves rather
 * than from the order an update arrived in, so two canvases showing the same
 * scene agree about what is on top.
 *
 * `locallyEditing` protects whatever is under this client's pointer from being
 * replaced mid-gesture — see locallyEditingIDs.
 */
export function mergeRemoteElements(
  local: readonly ExcalidrawElement[],
  remote: readonly ExcalidrawElement[],
  locallyEditing?: ReadonlySet<string>,
): MergeResult {
  const localByID = new Map(local.map((el) => [el.id, el]))
  const merged: ExcalidrawElement[] = []
  const accepted = new Map<string, number>()
  const contested = new Set<string>()
  const deferred = new Set<string>()
  const taken = new Set<string>()

  for (const remoteEl of remote) {
    const localEl = localByID.get(remoteEl.id)
    if (shouldKeepLocal(localEl, remoteEl, locallyEditing)) {
      const kept = localEl as ExcalidrawElement
      merged.push(kept)
      if (!shouldKeepLocal(kept, remoteEl)) deferred.add(kept.id)
      else if (!sameWrite(kept, remoteEl) && !locallyEditing?.has(kept.id)) contested.add(kept.id)
    } else {
      // A copy, never the room's own object. Y.Map hands back the stored
      // value itself, and Excalidraw changes the elements it holds in place
      // (an index fix-up on updateScene, every mutateElement after). Shared,
      // those writes landed in the local Y.Doc without ever being sent as an
      // update, so the room here no longer matched the room on the server —
      // and a later tombstone sweep read this canvas's own edit back as the
      // room's old value and hid a shape the user had just changed.
      merged.push({ ...remoteEl })
      accepted.set(remoteEl.id, remoteEl.version)
    }
    taken.add(remoteEl.id)
  }
  for (const localEl of local) {
    if (!taken.has(localEl.id)) merged.push(localEl)
  }
  // Sorted rather than left in arrival order: the merged array is the z-order,
  // and taking it from whichever peer's update happened to arrive first is how
  // two canvases end up disagreeing about what is on top.
  return { elements: merged.sort(compareElements), accepted, contested, deferred }
}

/** A fresh nonce for a version this client mints itself. */
function randomNonce(): number {
  return Math.floor(Math.random() * 2 ** 31)
}

/**
 * The elements whose gesture has ended and whose result the room would reject.
 *
 * The case: while one person resizes a shape, somebody else recolours it. The
 * resizing canvas drops the recolour (mergeRemoteElements' `deferred`), which
 * is right while the pointer is down. But if the recolour's version is above
 * the version the resize finishes at, every other canvas keeps the recolour
 * and rejects the resize, while the resizing canvas never re-reads the room
 * and keeps its own. The two screens disagree from then on.
 *
 * The person who just let go wins. Their copy is given a version above the
 * room's, and is then published like any other change. Losing a resize you
 * just made because of a colour change you never saw would be the worse
 * surprise. The colour change is visible to the person who made it until
 * the resize lands, so they can see it was overwritten.
 *
 * Returns the replacement copies, plus the ids that are settled and no longer
 * need watching. An id still under the pointer is neither.
 */
export function rebaseFinishedEdits(
  local: readonly ExcalidrawElement[],
  room: { get(id: string): ExcalidrawElement | undefined },
  deferred: ReadonlySet<string>,
  locallyEditing: ReadonlySet<string>,
): { rebased: ExcalidrawElement[]; settled: string[] } {
  const rebased: ExcalidrawElement[] = []
  const settled: string[] = []
  const localByID = new Map(local.map((el) => [el.id, el]))
  for (const id of deferred) {
    if (locallyEditing.has(id)) continue
    settled.push(id)
    const mine = localByID.get(id)
    const theirs = room.get(id)
    if (!mine || !theirs || shouldKeepLocal(mine, theirs)) continue
    rebased.push({
      ...mine,
      version: theirs.version + 1,
      versionNonce: randomNonce(),
      updated: Date.now(),
    } as ExcalidrawElement)
  }
  return { rebased, settled }
}

/**
 * Hide elements whose key was removed from the room.
 *
 * The sidecar removes tombstones of shapes erased long ago (see
 * hocuspocus/src/drawing-tombstones.ts). A canvas still holding the
 * tombstone loses nothing. A canvas that never received the erase, such as
 * a tab left open from before it, still shows the shape. A merge that only
 * looks at what the room *has* would never take it away, because a local
 * element with no room counterpart is kept. So the removal itself is the
 * signal: the shape is marked deleted here unless this canvas has changed it
 * since the copy that was removed. A change made after the removal is newer
 * work and is kept, and it is published like any other.
 *
 * `removed` maps each removed id to the value the room held, which is what
 * Y.js reports as a deleted key's old value.
 */
export function hideRemovedElements(
  elements: readonly ExcalidrawElement[],
  removed: ReadonlyMap<string, ExcalidrawElement | undefined>,
): ExcalidrawElement[] {
  if (removed.size === 0) return elements as ExcalidrawElement[]
  return elements.map((el) => {
    if (el.isDeleted || !removed.has(el.id)) return el
    const last = removed.get(el.id)
    if (last && el.version > last.version) return el
    return { ...el, isDeleted: true } as ExcalidrawElement
  })
}

/** The appState slots that say a continuous gesture is in progress. */
export interface GestureAppState extends LocalEditingAppState {
  selectedElementsAreBeingDragged?: boolean
  isResizing?: boolean
  isRotating?: boolean
}

/**
 * Whether the pointer is in the middle of something that changes the scene
 * on every frame: a stroke, a drag, a resize, a rotation or a line being
 * drawn. Writes are throttled only while this holds. A discrete change, such
 * as a colour, a deletion or a paste, is published at once, and so is the
 * final state of every gesture.
 */
export function isGestureActive(appState: GestureAppState): boolean {
  return (
    locallyEditingIDs(appState).size > 0 ||
    appState.selectedElementsAreBeingDragged === true ||
    appState.isResizing === true ||
    appState.isRotating === true
  )
}

/**
 * Push locally-changed elements into the shared scene.
 *
 * `lastSeen` maps element id to the version this client last wrote, and is
 * mutated in place. Diffing against it is what keeps a drag from writing every
 * element in the scene on every animation frame: Excalidraw hands us the whole
 * element array on each change, and only the ones whose version moved are ours
 * to publish.
 *
 * Returns the number of elements written, which the caller uses to decide
 * whether anything happened at all.
 */
export function writeLocalElements(
  ydoc: YDoc,
  elements: readonly ExcalidrawElement[],
  lastSeen: Map<string, number>,
): number {
  const shared = getElementsMap(ydoc)
  // Never a copy older than the room's. This canvas can hold one for a
  // moment, between the room taking a newer copy and the merge reaching the
  // canvas, and publishing it then would put the older copy back over the
  // newer one for everybody. The room's copy reaches this canvas with the
  // next merge.
  const changed = elements.filter(
    (el) => lastSeen.get(el.id) !== el.version && !((shared.get(el.id)?.version ?? -Infinity) > el.version),
  )
  if (changed.length === 0) return 0

  ydoc.transact(() => {
    for (const el of changed) {
      // Stored as a plain object: Yjs treats it as one opaque value, so an
      // element update is a single map entry replacement rather than a
      // field-by-field diff nobody asked for.
      shared.set(el.id, { ...el })
      lastSeen.set(el.id, el.version)
    }
  }, LOCAL_ORIGIN)

  return changed.length
}

/** Seed `lastSeen` from the shared scene, so the first local change after a
 * sync does not re-publish everything that arrived from it. */
export function seedLastSeen(elements: readonly ExcalidrawElement[]): Map<string, number> {
  return new Map(elements.map((el) => [el.id, el.version]))
}

/**
 * Publish the shared appState, field by field.
 *
 * Per-field rather than one object: two people changing the background and the
 * grid at the same moment would otherwise each write a whole appState and one
 * would silently revert the other's unrelated change.
 */
export function writeSharedAppState(ydoc: YDoc, next: SharedAppState): void {
  const stored = getAppStateMap(ydoc)
  const current = readSharedAppState(ydoc)

  const backgroundChanged =
    next.viewBackgroundColor !== undefined &&
    next.viewBackgroundColor !== current.viewBackgroundColor
  const gridChanged = next.gridSize !== undefined && next.gridSize !== current.gridSize
  if (!backgroundChanged && !gridChanged) return

  ydoc.transact(() => {
    if (backgroundChanged) stored.set(BACKGROUND_FIELD, next.viewBackgroundColor)
    if (gridChanged) stored.set(GRID_FIELD, next.gridSize)
  }, LOCAL_ORIGIN)
}

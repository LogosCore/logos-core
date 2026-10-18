// Bind a drawing page's canvas to its Y.js room.
//
// The pure half of this lives in drawing-scene.ts; what is here is the part
// that has to know about React, the Hocuspocus provider and Excalidraw's
// imperative API. Keeping the split means the merge rules can be tested
// without a browser, and this file stays small enough to read in one go.

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { Doc as YDoc } from "yjs"
import type { HocuspocusProvider } from "@hocuspocus/provider"
import { CaptureUpdateAction } from "@excalidraw/excalidraw"
import type {
  ExcalidrawImperativeAPI,
  AppState,
  BinaryFileData,
  BinaryFiles,
} from "@excalidraw/excalidraw/types"
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types"
import { toast } from "sonner"

import { getCursorColor } from "@/lib/cursor-colors"
import { holdRoom } from "@/hooks/use-hocuspocus"
import {
  LOCAL_ORIGIN,
  getAppStateMap,
  getElementsMap,
  hideRemovedElements,
  isGestureActive,
  locallyEditingIDs,
  mergeRemoteElements,
  readElements,
  readSharedAppState,
  rebaseFinishedEdits,
  seedLastSeen,
  writeLocalElements,
  writeSharedAppState,
} from "./drawing-scene"
import {
  POINTER_WRITE_INTERVAL_MS,
  SCENE_WRITE_INTERVAL_MS,
  createThrottle,
  type Throttle,
} from "./drawing-throttle"
import {
  toExcalidrawCollaborators,
  type DrawingAwarenessState,
} from "./drawing-collaborators"
import {
  applyImageIDs,
  pendingImages,
  publishableElements,
  uploadPendingImages,
  uploadRetryDelay,
  wikiImageFiles,
  wikiImageIDs,
  wikiImageURL,
} from "./drawing-images"

interface UseDrawingSyncArgs {
  /** The page this canvas belongs to. Images are uploaded against it, which is
   * also what scopes them for the attachment sweeper. */
  documentId: string
  ydoc: YDoc
  provider: HocuspocusProvider | null
  /** Whether this client may write. Viewers still receive live updates; the
   * server rejects any Y.js update they emit regardless, so this is about not
   * lying to the user rather than about enforcement. */
  isEditor: boolean
  user: { userId: string; username: string } | null
}

interface PointerPayload {
  pointer: { x: number; y: number; tool: "pointer" | "laser" }
  button: "up" | "down"
}

interface UseDrawingSyncReturn {
  /** The canvas's API once it has mounted, for hooks of the canvas's own. */
  api: ExcalidrawImperativeAPI | null
  onExcalidrawAPI: (api: ExcalidrawImperativeAPI) => void
  onChange: (
    elements: readonly ExcalidrawElement[],
    appState: AppState,
    files: BinaryFiles,
  ) => void
  onPointerUpdate: (payload: PointerPayload) => void
  /** The scene as it stood when the canvas mounted. Read once — Excalidraw
   * takes initialData at mount and everything after that arrives by
   * updateScene. */
  initialElements: ExcalidrawElement[]
  initialBackgroundColor: string | undefined
  /** Image files for the scene as it arrived, pointing at their wiki URLs. */
  initialFiles: BinaryFiles
}

export function useDrawingSync({
  documentId,
  ydoc,
  provider,
  isEditor,
  user,
}: UseDrawingSyncArgs): UseDrawingSyncReturn {
  // Held in state rather than a ref so effects can depend on it: the canvas
  // hands its API over during mount, after the first render, and a ref would
  // leave those effects to run once against null and never re-run.
  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null)

  // Presence channel. Carries the cursor, the laser's press state and the
  // selection — everything about a person rather than about the drawing. The
  // "user" field is written in the same shape the prose editor uses
  // (wiki-editor.tsx), so one person is coloured identically whether they are
  // typing on a page or drawing on a diagram.
  const awareness = provider?.awareness ?? null

  // Read once, at mount. The pane only renders the canvas after the room has
  // synced, so by the time this runs the scene is whatever the server had.
  const [initial] = useState(() => ({
    elements: readElements(ydoc),
    appState: readSharedAppState(ydoc),
  }))

  // Element id to the version this client last published. Diffing against it
  // is what stops a drag republishing the whole scene on every frame.
  //
  // Seeded from the scene that arrived with the initial sync, so the first
  // local change does not re-send all of it straight back to everyone. Seeded
  // lazily rather than in the useState initialiser above because writing a ref
  // during render is what it looks like: a second piece of state being
  // initialised where React cannot see it.
  const lastSeenRef = useRef<Map<string, number> | null>(null)
  const lastSeen = useCallback(() => {
    lastSeenRef.current ??= seedLastSeen(initial.elements)
    return lastSeenRef.current
  }, [initial])

  const onExcalidrawAPI = useCallback((next: ExcalidrawImperativeAPI) => {
    setApi(next)
  }, [])

  // Elements this canvas kept through a gesture over a newer copy from the
  // room. Checked when the gesture ends; see rebaseFinishedEdits.
  const deferredRef = useRef<Set<string>>(new Set())
  // The scene as Excalidraw last reported it. The writer reads the live scene
  // from the API; this copy is only for the last flush on unmount, when the
  // canvas may already have discarded its scene.
  const latestRef = useRef<readonly ExcalidrawElement[] | null>(null)
  // The scene writer, once the canvas is up and this client may write. A ref
  // because both onChange and the room observer ask it for a write.
  const writerRef = useRef<Throttle | null>(null)

  // Canvas to shared scene, rate-limited while a gesture is in progress (see
  // drawing-throttle.ts). Every run publishes whatever the canvas holds at
  // that moment, never a copy captured earlier, so a write that waited cannot
  // put something staler than the canvas into the room.
  useEffect(() => {
    if (!api || !isEditor) return
    let unmounting = false

    const write = () => {
      let elements = unmounting ? latestRef.current : api.getSceneElementsIncludingDeleted()
      if (!elements) return
      const seen = lastSeen()

      if (!unmounting && deferredRef.current.size > 0) {
        const editing = locallyEditingIDs(api.getAppState())
        const { rebased, settled } = rebaseFinishedEdits(
          elements,
          getElementsMap(ydoc),
          deferredRef.current,
          editing,
        )
        for (const id of settled) deferredRef.current.delete(id)
        if (rebased.length > 0) {
          const byID = new Map(rebased.map((el) => [el.id, el]))
          api.updateScene({
            elements: elements.map((el) => byID.get(el.id) ?? el),
            captureUpdate: CaptureUpdateAction.NEVER,
          })
          elements = api.getSceneElementsIncludingDeleted()
        }
      }

      // Images still uploading stay on this canvas until they are stored;
      // see publishableElements. adoptImages rewrites them to their wiki id,
      // which bumps their version, and the write after that publishes them.
      writeLocalElements(ydoc, publishableElements(elements, seen), seen)
    }

    const writer = createThrottle(write, SCENE_WRITE_INTERVAL_MS)
    writerRef.current = writer
    // A write still waiting when the tab goes away would be lost, so it is
    // sent right away. Both events, because a closed mobile tab never fires
    // pagehide.
    const onPageHide = () => writer.flush()
    const onVisibility = () => {
      if (document.visibilityState === "hidden") writer.flush()
    }
    window.addEventListener("pagehide", onPageHide)
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      window.removeEventListener("pagehide", onPageHide)
      document.removeEventListener("visibilitychange", onVisibility)
      unmounting = true
      writer.flush()
      writerRef.current = null
    }
  }, [ydoc, api, isEditor, lastSeen])

  // Shared scene to canvas.
  useEffect(() => {
    if (!api) return
    const shared = getElementsMap(ydoc)
    // Ids whose key left the room since the last merge, with the value the
    // room held. See hideRemovedElements.
    const removed = new Map<string, ExcalidrawElement | undefined>()
    let frame = 0

    // At most once per frame. A peer mid-gesture sends up to twenty updates
    // a second, and each merge reads, sorts and re-renders the whole scene.
    // Doing that more often than the screen can show is work nobody sees.
    const apply = () => {
      frame = 0
      // Including deleted: tombstones are how Excalidraw learns that somebody
      // else erased a shape. Merging against the non-deleted view would keep
      // resurrecting it.
      //
      // The appState is read at merge time rather than captured: what the
      // pointer is holding changes between one remote update and the next, and
      // this handler outlives any one gesture.
      const { elements, accepted, contested, deferred } = mergeRemoteElements(
        api.getSceneElementsIncludingDeleted(),
        readElements(ydoc),
        locallyEditingIDs(api.getAppState()),
      )
      // The versions taken from the room are now what this client holds, so
      // record them — otherwise the next local change republishes everything
      // that just arrived, and two clients bounce the same elements back and
      // forth. Only those: an element whose local copy won is still unpublished
      // at its local version, and marking it seen would strand it here.
      const seen = lastSeen()
      for (const [id, version] of accepted) seen.set(id, version)
      // A local copy that beats the room's by the merge rule, but lost to it
      // in the room, is republished: forgetting it was published is what
      // makes the next write send it.
      for (const id of contested) seen.delete(id)
      for (const id of deferred) deferredRef.current.add(id)

      const shown = hideRemovedElements(elements, removed)
      removed.clear()

      // A peer's image arrives as an element carrying a wiki image id, with no
      // file entry on this client — the canvas would render an empty frame.
      // Register the ones we do not have; the bytes come from the URL.
      registerWikiImages(api, shown)

      // Never into the undo history. Left to Excalidraw's default, a peer's
      // change was folded into this user's next undo step, so Ctrl+Z reverted
      // the peer's edit too — at a new version, which then published it.
      api.updateScene({ elements: shown, captureUpdate: CaptureUpdateAction.NEVER })
      if (contested.size > 0) writerRef.current?.request(true)
    }

    const handler = (
      event: { changes: { keys: Map<string, { action: string; oldValue?: unknown }> } },
      transaction: { origin: unknown },
    ) => {
      // Our own writes come back through here too. Applying them would at best
      // waste a render and at worst fight the user's pointer mid-drag.
      if (transaction.origin === LOCAL_ORIGIN) return
      for (const [id, change] of event.changes.keys) {
        if (change.action === "delete") removed.set(id, change.oldValue as ExcalidrawElement | undefined)
      }
      frame ||= requestAnimationFrame(apply)
    }

    shared.observe(handler)
    return () => {
      shared.unobserve(handler)
      cancelAnimationFrame(frame)
    }
  }, [ydoc, api, lastSeen])

  // Shared appState to canvas. Without this a peer's background change never
  // reached this canvas — and worse, this canvas's next onChange compared its
  // own stale background with the shared one, found them different, and wrote
  // its own back over the peer's. A restored backup's background was undone
  // the same way by whoever had the page open.
  useEffect(() => {
    if (!api) return
    const shared = getAppStateMap(ydoc)

    const handler = (_event: unknown, transaction: { origin: unknown }) => {
      if (transaction.origin === LOCAL_ORIGIN) return
      const { viewBackgroundColor, gridSize } = readSharedAppState(ydoc)
      // Only fields the room actually holds. One that is absent was never
      // chosen, and the canvas's own default is the right thing to keep.
      const next: Partial<AppState> = {}
      if (viewBackgroundColor !== undefined) next.viewBackgroundColor = viewBackgroundColor
      if (typeof gridSize === "number") next.gridSize = gridSize
      if (Object.keys(next).length > 0) {
        api.updateScene({ appState: next as AppState, captureUpdate: CaptureUpdateAction.NEVER })
      }
    }

    shared.observe(handler)
    return () => shared.unobserve(handler)
  }, [ydoc, api])

  // Images whose upload is in flight. Tracked so a slow upload is not started
  // again by every subsequent change event — onChange fires continuously while
  // the user keeps drawing — and so leaving the page mid-upload can be caught.
  const uploadingRef = useRef<Set<string>>(new Set())
  // Local fileId to the wiki id it was stored as. An undo can hand back an
  // element carrying the local id after it was rewritten; this puts the
  // stored id back on it instead of uploading the same bytes a second time.
  const adoptedRef = useRef<Map<string, string>>(new Map())
  // Local fileId to the failures its upload has had, and when it may be tried
  // again — Infinity once the server has refused it for good. See
  // uploadRetryDelay.
  const failedRef = useRef<Map<string, { attempts: number; retryAt: number }>>(new Map())
  // Whether the canvas is still mounted. An upload can outlive it — see the
  // hold in adoptImages — and then has to publish without the canvas.
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const adoptImages = useCallback(
    async (elements: readonly ExcalidrawElement[], files: BinaryFiles) => {
      if (!api || !isEditor) return

      const now = Date.now()
      const waiting = pendingImages(elements, files).filter(
        (image) =>
          !uploadingRef.current.has(image.fileId) &&
          (failedRef.current.get(image.fileId)?.retryAt ?? 0) <= now,
      )
      const known = waiting.filter((image) => adoptedRef.current.has(image.fileId))
      const pending = waiting.filter((image) => !adoptedRef.current.has(image.fileId))
      if (known.length === 0 && pending.length === 0) return
      for (const image of pending) uploadingRef.current.add(image.fileId)

      // Keeps the room connected if the user leaves the page mid-upload, so
      // the image can still be published below once it is stored.
      const release = pending.length > 0 ? holdRoom(ydoc) : null
      let mapping: Map<string, string>
      try {
        mapping = await uploadPendingImages(documentId, pending, (fileId, error) => {
          // Released for a later change to retry, after a delay. The image
          // keeps rendering locally in the meantime — but only locally, since
          // it is not published until it is stored — so the message says so.
          // Told once per image: a retry that fails again says nothing new.
          uploadingRef.current.delete(fileId)
          const attempts = (failedRef.current.get(fileId)?.attempts ?? 0) + 1
          const delay = uploadRetryDelay(error, attempts)
          failedRef.current.set(fileId, {
            attempts,
            retryAt: delay === null ? Infinity : Date.now() + delay,
          })
          const reason = error instanceof Error ? error.message : ""
          if (delay === null) {
            toast.error("An image on this drawing was refused.", {
              description: `Only you can see it, and it will not be stored. Remove it and try another file. ${reason}`,
            })
          } else if (attempts === 1) {
            toast.error("Could not store an image on this drawing.", {
              description: `Only you can see it until it is stored; it will be retried. ${reason}`,
            })
          }
        })
        for (const [fileId, id] of mapping) {
          uploadingRef.current.delete(fileId)
          failedRef.current.delete(fileId)
          adoptedRef.current.set(fileId, id)
        }
        for (const image of known) {
          mapping.set(image.fileId, adoptedRef.current.get(image.fileId) as string)
        }

        // The canvas went away while the upload ran. Its last flush left these
        // images out (see publishableElements), so publish them from the scene
        // it last reported, straight into the room the hold kept open.
        if (!mountedRef.current) {
          const last = latestRef.current
          if (mapping.size > 0 && last) {
            const seen = lastSeen()
            writeLocalElements(ydoc, publishableElements(applyImageIDs(last, mapping), seen), seen)
          }
          return
        }
      } finally {
        release?.()
      }
      if (mapping.size === 0) return

      // Register the stored copies before rewriting the elements, so there is
      // no frame in which an element points at a file the canvas cannot find.
      api.addFiles(
        [...mapping.values()].map(
          (id) =>
            ({
              id,
              dataURL: wikiImageURL(id),
              mimeType: "image/png",
              created: Date.now(),
            }) as unknown as BinaryFileData,
        ),
      )
      api.updateScene({
        elements: applyImageIDs(api.getSceneElementsIncludingDeleted(), mapping),
        // Bookkeeping, not an edit: an undo should not swap the stored id back
        // for the local one (adoptedRef covers it if a redo hands one back).
        captureUpdate: CaptureUpdateAction.NEVER,
      })
    },
    [api, documentId, isEditor, ydoc, lastSeen],
  )

  // An image only exists on this canvas until its upload finishes, so leaving
  // mid-upload loses it. Ask first, the way an unsaved form would. The handler
  // is always attached and checks at the moment of leaving, because uploads
  // start and finish without a render to re-run an effect on.
  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (uploadingRef.current.size === 0) return
      e.preventDefault()
      // Still required by some browsers to show the prompt at all.
      e.returnValue = ""
    }
    window.addEventListener("beforeunload", onBeforeUnload)
    return () => window.removeEventListener("beforeunload", onBeforeUnload)
  }, [])

  // What this client has selected, as last published. Selection changes far
  // less often than the scene does, and onChange fires on every mutation —
  // including every frame of a drag — so without this the canvas would
  // broadcast an awareness update per frame to say nothing had changed.
  const lastSelectionRef = useRef("")

  const publishSelection = useCallback(
    (appState: AppState) => {
      if (!awareness) return
      const selected = appState.selectedElementIds ?? {}
      // Sorted so the key describes the selection rather than the order
      // Excalidraw happened to record it in.
      const key = Object.keys(selected).sort().join(",")
      if (key === lastSelectionRef.current) return
      lastSelectionRef.current = key

      awareness.setLocalState({
        ...(awareness.getLocalState() ?? {}),
        selectedElementIds: selected,
      })
    },
    [awareness],
  )

  // Canvas to shared scene.
  const onChange = useCallback(
    (elements: readonly ExcalidrawElement[], appState: AppState, files: BinaryFiles) => {
      // Ungated: a selection is presence, not content, and a viewer pointing at
      // a shape is exactly as worth seeing as a viewer's cursor — which is
      // already published for them.
      publishSelection(appState)

      if (!isEditor) return
      latestRef.current = elements
      // Mid-gesture changes wait for the throttle; anything else, including
      // the change that ends a gesture, goes out at once.
      writerRef.current?.request(!isGestureActive(appState))
      writeSharedAppState(ydoc, {
        viewBackgroundColor: appState.viewBackgroundColor,
        gridSize: appState.gridSize,
      })
      void adoptImages(elements, files)
    },
    [ydoc, isEditor, adoptImages, publishSelection],
  )

  const color = getCursorColor(user?.userId ?? "anon")

  useEffect(() => {
    if (!awareness) return
    awareness.setLocalStateField("user", {
      name: user?.username ?? "Anonymous",
      color,
    })
  }, [awareness, color, user?.username])

  // Other people's cursors. Excalidraw takes collaborators through
  // updateScene rather than as a prop, so this is an effect rather than a
  // value handed back to the component.
  useEffect(() => {
    if (!awareness || !api) return

    let frame = 0
    const update = () => {
      frame = 0
      api.updateScene({
        collaborators: toExcalidrawCollaborators(
          awareness.getStates() as Map<number, DrawingAwarenessState>,
          awareness.clientID,
        ),
      })
    }
    // Once per frame at most: every peer's cursor moves are a change event
    // each, and each re-renders the canvas.
    const onChange = () => {
      frame ||= requestAnimationFrame(update)
    }

    update()
    awareness.on("change", onChange)
    return () => {
      awareness.off("change", onChange)
      cancelAnimationFrame(frame)
    }
  }, [awareness, api])

  // The pointer as last reported, and the press state last broadcast.
  const pointerRef = useRef<PointerPayload | null>(null)
  const sentButtonRef = useRef<PointerPayload["button"] | null>(null)
  const pointerWriterRef = useRef<Throttle | null>(null)
  useEffect(() => {
    if (!awareness) return
    const writer = createThrottle(() => {
      const payload = pointerRef.current
      if (!payload) return
      sentButtonRef.current = payload.button
      // Pointer position is presence, not content: it rides on awareness and
      // is never persisted. Someone's cursor should not survive their session.
      //
      // The press state travels with it, because a remote laser trail is drawn
      // only while the peer is pressing — see DrawingAwarenessState.button.
      //
      // Both go in a single setLocalState rather than two setLocalStateField
      // calls: each field write broadcasts an awareness update of its own.
      // Merging over the current state keeps the `user` field, which
      // setLocalState would otherwise replace.
      awareness.setLocalState({
        ...(awareness.getLocalState() ?? {}),
        // The laser colour rides along so a peer's trail of ours is our
        // colour whatever Excalidraw would pick — see DrawingAwarenessState.
        pointer: { ...payload.pointer, laserColor: color },
        button: payload.button,
      })
    }, POINTER_WRITE_INTERVAL_MS)
    pointerWriterRef.current = writer
    return () => {
      writer.cancel()
      pointerWriterRef.current = null
    }
  }, [awareness, color])

  const onPointerUpdate = useCallback(
    (payload: PointerPayload) => {
      pointerRef.current = payload
      // A press or release goes out at once: a peer draws our laser trail only
      // while we are pressing, so a delayed release leaves it hanging.
      pointerWriterRef.current?.request(payload.button !== sentButtonRef.current)
    },
    [],
  )

  return useMemo(
    () => ({
      api,
      onExcalidrawAPI,
      onChange,
      onPointerUpdate,
      initialElements: initial.elements,
      initialBackgroundColor: initial.appState.viewBackgroundColor,
      initialFiles: wikiImageFiles(initial.elements),
    }),
    [api, onExcalidrawAPI, onChange, onPointerUpdate, initial],
  )
}

/**
 * Make sure every wiki image the scene references has a file entry.
 *
 * Excalidraw renders an image element by looking its fileId up in the files
 * map; an element whose file is missing draws as an empty placeholder with no
 * error. That is the normal state for anything that arrived from a peer, so
 * this runs on every remote merge rather than only at load.
 */
function registerWikiImages(
  api: ExcalidrawImperativeAPI,
  elements: readonly ExcalidrawElement[],
): void {
  const known = api.getFiles()
  const missing = wikiImageIDs(elements).filter((id) => !known[id as keyof BinaryFiles])
  if (missing.length === 0) return

  api.addFiles(
    missing.map(
      (id) =>
        ({
          id,
          dataURL: wikiImageURL(id),
          mimeType: "image/png",
          created: Date.now(),
        }) as unknown as BinaryFileData,
    ),
  )
}

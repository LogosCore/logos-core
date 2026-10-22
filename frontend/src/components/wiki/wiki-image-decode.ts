// Turns a TIFF or HEIC attachment into something an <img> can show.
//
// Chrome and Firefox decode neither format, so the file card fetches the
// bytes, has a worker decode them (wiki-image-decode-*.worker.ts) and points
// its thumbnail and lightbox at the PNG or JPEG that comes back. The decoders
// are loaded with their worker, the first time a card on screen needs one.
//
// Results are kept by URL for the life of the page: the thumbnail and the
// lightbox share one decode, and so does a card that remounts as the editor
// re-renders. A failure is kept too, so a file that will not decode is not
// fetched again on every scroll.
//
// The HTML export's viewer swaps this module for viewer-image-decode.ts, which
// declines every file: see the alias in vite.viewer.config.ts.

import type { DecodedImageFormat } from "./wiki-file-preview-image"
import type { DecodeRequest, DecodeResponse } from "./wiki-image-decode-protocol"

/** Past this the fetch alone takes a while and the decode holds hundreds of
 *  megabytes; the card keeps its icon and the file stays a download. */
export const MAX_DECODE_BYTES = 40 * 1024 * 1024

const results = new Map<string, Promise<string>>()

/** An object URL for the decoded image, or a rejection with a reason. */
export function decodeImage(url: string, format: DecodedImageFormat, size: number): Promise<string> {
  const key = `${format}:${url}`
  let result = results.get(key)
  if (!result) {
    result = size > MAX_DECODE_BYTES ? Promise.reject(new Error("too large to preview")) : fetchAndDecode(url, format)
    // Kept even when it fails (see above); the catch only stops an
    // unhandled-rejection report for a result nobody is awaiting yet.
    result.catch(() => {})
    results.set(key, result)
  }
  return result
}

async function fetchAndDecode(url: string, format: DecodedImageFormat): Promise<string> {
  const response = await fetch(url, { credentials: "same-origin" })
  if (!response.ok) throw new Error(`could not fetch the file (${response.status})`)
  const blob = await send(format, await response.arrayBuffer())
  return URL.createObjectURL(blob)
}

interface FormatWorker {
  worker: Worker
  pending: Map<number, { resolve: (blob: Blob) => void; reject: (error: Error) => void }>
}

const workers = new Map<DecodedImageFormat, FormatWorker>()
let nextId = 1

function spawn(format: DecodedImageFormat): Worker {
  // Literal URLs, one per format: Vite finds workers by this exact shape.
  return format === "heic"
    ? new Worker(new URL("./wiki-image-decode-heic.worker.ts", import.meta.url), { type: "module" })
    : new Worker(new URL("./wiki-image-decode-tiff.worker.ts", import.meta.url), { type: "module" })
}

function workerFor(format: DecodedImageFormat): FormatWorker {
  const existing = workers.get(format)
  if (existing) return existing
  if (typeof Worker === "undefined") throw new Error("this browser cannot decode images in the background")
  const entry: FormatWorker = { worker: spawn(format), pending: new Map() }
  entry.worker.onmessage = (event: MessageEvent<DecodeResponse>) => {
    const reply = event.data
    const waiting = entry.pending.get(reply.id)
    if (!waiting) return
    entry.pending.delete(reply.id)
    if (reply.ok) waiting.resolve(reply.blob)
    else waiting.reject(new Error(reply.error))
  }
  // A worker that fails as a whole (its module did not load, it ran out of
  // memory) answers nothing, so everything waiting on it is released here and
  // the next request starts a fresh one.
  entry.worker.onerror = (event) => {
    event.preventDefault()
    const error = new Error(event.message || "the image decoder stopped")
    for (const waiting of entry.pending.values()) waiting.reject(error)
    entry.pending.clear()
    entry.worker.terminate()
    workers.delete(format)
  }
  workers.set(format, entry)
  return entry
}

function send(format: DecodedImageFormat, bytes: ArrayBuffer): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const entry = workerFor(format)
    const id = nextId++
    entry.pending.set(id, { resolve, reject })
    const request: DecodeRequest = { id, bytes }
    entry.worker.postMessage(request, [bytes])
  })
}

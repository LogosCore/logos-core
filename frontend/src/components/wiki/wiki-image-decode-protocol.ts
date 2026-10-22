// What wiki-image-decode.ts and its two workers say to each other, and the
// half of the work both workers share: RGBA pixels in, an image file out.
//
// One worker per format rather than one for both. The HEIC decoder is libheif
// compiled to wasm, about 1.5 MB; a page with only a TIFF on it should not
// download that, and Vite's worker builds cannot split a chunk out of one
// worker.

export interface DecodeRequest {
  id: number
  bytes: ArrayBuffer
}

export type DecodeResponse =
  | { id: number; ok: true; blob: Blob; width: number; height: number }
  | { id: number; ok: false; error: string }

/** Larger than any camera's photo by a wide margin. The pixels are held four
 *  bytes each, twice over while encoding, so this caps a decode near 400 MB. */
export const MAX_DECODED_PIXELS = 50_000_000

export function checkSize(width: number, height: number): void {
  if (!(width > 0 && height > 0)) throw new Error("the image has no size")
  if (width * height > MAX_DECODED_PIXELS) {
    throw new Error(`the image is too large to preview (${width}×${height})`)
  }
}

/**
 * Encodes decoded pixels for an <img>. PNG when any pixel is transparent,
 * since JPEG has no alpha, and JPEG otherwise: these are mostly photos and
 * scans, and a PNG of a 12-megapixel photo is slow to write and several times
 * the size.
 */
export async function encodePixels(
  rgba: Uint8ClampedArray<ArrayBuffer>,
  width: number,
  height: number,
): Promise<Blob> {
  const canvas = new OffscreenCanvas(width, height)
  const context = canvas.getContext("2d")
  if (!context) throw new Error("no 2D canvas in this browser")
  context.putImageData(new ImageData(rgba, width, height), 0, 0)
  return canvas.convertToBlob(
    hasTransparency(rgba) ? { type: "image/png" } : { type: "image/jpeg", quality: 0.92 },
  )
}

export function hasTransparency(rgba: Uint8ClampedArray): boolean {
  for (let i = 3; i < rgba.length; i += 4) {
    if (rgba[i] !== 255) return true
  }
  return false
}

/** The worker side of the protocol: decode is the format's own half. */
export function serveDecodes(
  scope: { onmessage: ((event: MessageEvent<DecodeRequest>) => void) | null; postMessage: (reply: DecodeResponse) => void },
  decode: (bytes: Uint8Array) => Promise<{ rgba: Uint8ClampedArray<ArrayBuffer>; width: number; height: number }>,
): void {
  scope.onmessage = (event) => {
    const { id, bytes } = event.data
    void (async () => {
      try {
        const { rgba, width, height } = await decode(new Uint8Array(bytes))
        const blob = await encodePixels(rgba, width, height)
        scope.postMessage({ id, ok: true, blob, width, height })
      } catch (error) {
        scope.postMessage({ id, ok: false, error: error instanceof Error ? error.message : String(error) })
      }
    })()
  }
}

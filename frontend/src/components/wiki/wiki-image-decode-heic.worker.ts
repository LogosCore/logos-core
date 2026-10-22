// Decodes HEIC and HEIF attachments for the file card, off the main thread.
// libheif compiled to wasm, with the binary embedded in the module so there is
// no second file to locate. See wiki-image-decode-protocol.ts.

import libheif from "libheif-js/libheif-wasm/libheif-bundle.mjs"

import { checkSize, serveDecodes } from "./wiki-image-decode-protocol"

// One decoder for the worker's life: each decode() frees the context the
// previous one left behind, where a decoder per request would leak it.
const decoder = new (libheif().HeifDecoder)()

serveDecodes(self as unknown as Parameters<typeof serveDecodes>[0], async (bytes) => {
  // An iPhone photo is one image; a burst or a live photo holds several.
  // The first is the one the camera shows.
  const images = decoder.decode(bytes)
  if (images.length === 0) throw new Error("no image in this HEIC")
  try {
    const image = images[0]
    const width = image.get_width()
    const height = image.get_height()
    checkSize(width, height)
    const target = { data: new Uint8ClampedArray(width * height * 4), width, height }
    await new Promise<void>((resolve, reject) => {
      image.display(target, (done) => (done ? resolve() : reject(new Error("could not decode this HEIC"))))
    })
    return { rgba: target.data, width, height }
  } finally {
    for (const image of images) image.free()
  }
})

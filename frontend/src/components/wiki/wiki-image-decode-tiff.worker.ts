// Decodes TIFF attachments for the file card, off the main thread. See
// wiki-image-decode-protocol.ts.

import * as UTIF from "utif2"

import { checkSize, serveDecodes } from "./wiki-image-decode-protocol"

/** A tag's first value: UTIF stores every tag as an array. */
function tag(ifd: UTIF.IFD, id: number): number {
  const value = ifd[`t${id}`]
  return Array.isArray(value) && typeof value[0] === "number" ? value[0] : 0
}

serveDecodes(self as unknown as Parameters<typeof serveDecodes>[0], async (bytes) => {
  const buffer = bytes.buffer as ArrayBuffer
  // A file can carry several images: pages, or a reduced-size preview next to
  // the full one. Show the largest, which is what a viewer opens on.
  const pages = UTIF.decode(buffer).filter((ifd) => tag(ifd, 256) > 0 && tag(ifd, 257) > 0)
  if (pages.length === 0) throw new Error("no image in this TIFF")
  const page = pages.reduce((a, b) => (tag(b, 256) * tag(b, 257) > tag(a, 256) * tag(a, 257) ? b : a))
  // Checked from the header, before the pixels are inflated.
  checkSize(tag(page, 256), tag(page, 257))
  UTIF.decodeImage(buffer, page)
  const rgba = new Uint8ClampedArray(UTIF.toRGBA8(page))
  return { rgba, width: page.width, height: page.height }
})

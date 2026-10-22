import { describe, expect, test } from "vitest"
import { imagePreviewFormat, isPreviewableImage } from "@/components/wiki/wiki-file-preview-image"

describe("imagePreviewFormat", () => {
  test("the browser draws what the backend serves inline", () => {
    for (const ct of [
      "image/png",
      "image/jpeg",
      "image/gif",
      "image/webp",
      "image/avif",
      "image/bmp",
      "image/x-icon",
      "image/vnd.microsoft.icon",
    ]) {
      expect(imagePreviewFormat(ct, "shot.bin")).toBe("native")
    }
  })

  test("SVG is drawn natively: an <img> never runs its scripts", () => {
    // The new-tab view is sandboxed by the file controller's CSP; see
    // previewAllowedContentTypes in wiki_file_controller.go.
    expect(imagePreviewFormat("image/svg+xml", "logo.svg")).toBe("native")
  })

  test("TIFF and HEIC are decoded first, whichever name the type goes by", () => {
    expect(imagePreviewFormat("image/tiff", "scan.tif")).toBe("tiff")
    expect(imagePreviewFormat("image/heic", "IMG_0001.HEIC")).toBe("heic")
    // What the backend's sniffer calls an iPhone photo.
    expect(imagePreviewFormat("image/heif", "IMG_0001.HEIC")).toBe("heic")
  })

  test("falls back to the extension only when the type says nothing", () => {
    expect(imagePreviewFormat("application/octet-stream", "shot.PNG")).toBe("native")
    expect(imagePreviewFormat("", "shot.jpeg")).toBe("native")
    expect(imagePreviewFormat("application/octet-stream", "favicon.ico")).toBe("native")
    expect(imagePreviewFormat("application/octet-stream", "scan.TIFF")).toBe("tiff")
    expect(imagePreviewFormat("", "photo.heif")).toBe("heic")
    expect(imagePreviewFormat("application/octet-stream", "notes.txt")).toBeNull()
    expect(imagePreviewFormat("application/octet-stream", "noext")).toBeNull()
  })

  test("a positively-identified type is not overridden by the extension", () => {
    // No decoder for it, and a .png name must not talk us into a broken tile.
    expect(imagePreviewFormat("image/x-portable-pixmap", "frame.png")).toBeNull()
    expect(imagePreviewFormat("text/html", "page.png")).toBeNull()
  })

  test("isPreviewableImage is any format at all", () => {
    expect(isPreviewableImage("image/png", "a.png")).toBe(true)
    expect(isPreviewableImage("image/tiff", "a.tif")).toBe(true)
    expect(isPreviewableImage("application/zip", "a.zip")).toBe(false)
  })
})

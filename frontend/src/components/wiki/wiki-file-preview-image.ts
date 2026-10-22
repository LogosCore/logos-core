// Which image attachments the file card shows as a picture, and how.
//
// Most formats the browser draws itself: the thumbnail and the lightbox point
// an <img> at the inline URL. TIFF and HEIC it does not (only Safari decodes
// either), so those are converted in a worker first — see wiki-image-decode.ts.

/** Types the browser draws from the inline URL. Mirrors
 *  previewAllowedContentTypes in wiki_file_controller.go, which must serve
 *  these with an inline disposition for the new-tab preview to display rather
 *  than download.
 *
 *  SVG is among them. It is a scriptable document rather than a raster, but
 *  none of the three ways the card shows it can run a script: an <img> never
 *  executes one, and the new-tab view gets the file controller's sandbox CSP,
 *  which also cuts it off from the network. The inline wiki image node has
 *  served SVG that way all along. */
const NATIVE_IMAGE_CONTENT_TYPES = new Set<string>([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/avif",
  "image/bmp",
  "image/svg+xml",
  "image/x-icon",
  "image/vnd.microsoft.icon",
])

/** Types decoded to a PNG or JPEG in a worker before display. */
const DECODED_IMAGE_CONTENT_TYPES: Record<string, DecodedImageFormat> = {
  "image/tiff": "tiff",
  "image/tiff-fx": "tiff",
  "image/heic": "heic",
  "image/heif": "heic",
  "image/heic-sequence": "heic",
  "image/heif-sequence": "heic",
}

/** Extension fallback for when MIME sniffing lands on something generic —
 *  the same problem docx/xlsx have in wiki-file-preview-source.ts. An image
 *  stored as application/octet-stream still deserves its thumbnail. */
const NATIVE_IMAGE_EXTENSIONS = new Set<string>([
  "png",
  "jpg",
  "jpeg",
  "gif",
  "webp",
  "avif",
  "bmp",
  "svg",
  "ico",
])

const DECODED_IMAGE_EXTENSIONS: Record<string, DecodedImageFormat> = {
  tif: "tiff",
  tiff: "tiff",
  heic: "heic",
  heif: "heic",
}

export type DecodedImageFormat = "tiff" | "heic"

/** "native" for an <img> pointed at the file, a format for one that needs
 *  decoding first, null for an attachment that is not an image we can show. */
export type ImagePreviewFormat = "native" | DecodedImageFormat

/**
 * How this attachment renders as an image: thumbnail on the card, full size in
 * the lightbox. Null when it does not.
 *
 * Content type wins; the extension is consulted only when the type is
 * unrecognised — a file named .png that the backend identified as text stays
 * text.
 */
export function imagePreviewFormat(
  contentType: string,
  filename: string,
): ImagePreviewFormat | null {
  if (NATIVE_IMAGE_CONTENT_TYPES.has(contentType)) return "native"
  const decoded = DECODED_IMAGE_CONTENT_TYPES[contentType]
  if (decoded) return decoded
  // Only fall back when the type says nothing useful. An "image/x-portable-
  // pixmap" we have no decoder for must not be rescued by its extension.
  if (contentType !== "" && contentType !== "application/octet-stream") {
    return null
  }
  const dot = filename.lastIndexOf(".")
  const ext = dot === -1 ? "" : filename.slice(dot + 1).toLowerCase()
  if (NATIVE_IMAGE_EXTENSIONS.has(ext)) return "native"
  return DECODED_IMAGE_EXTENSIONS[ext] ?? null
}

/** True when this attachment should render as an image at all. */
export function isPreviewableImage(contentType: string, filename: string): boolean {
  return imagePreviewFormat(contentType, filename) !== null
}

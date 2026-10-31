// How a file is previewed, from its content type, name and size: the
// question the wiki's attachment card and the session console's file view
// both ask before showing one.

import { imagePreviewFormat, type ImagePreviewFormat } from "./image"
import { detectMediaPreviewKind, type MediaPreviewKind } from "./media"
import { detectInlinePreviewKind, type InlinePreviewKind } from "./source"

/** Content types the browser can render inline without executing scripts.
 *  Must stay in sync with previewAllowedContentTypes in wiki_file_controller.go,
 *  which serves both wiki files and minion files. */
export const PREVIEW_ALLOWED_CONTENT_TYPES = new Set<string>([
  "application/pdf",
  "text/plain",
  "text/markdown",
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/avif",
  "image/bmp",
  "image/svg+xml",
  "image/x-icon",
  "image/vnd.microsoft.icon",
  "video/webm",
  "video/mp4",
  "video/ogg",
  "video/quicktime",
  "video/x-m4v",
  "audio/mpeg",
  "audio/ogg",
  "audio/wav",
  "audio/x-wav",
  "audio/webm",
  "audio/mp4",
  "audio/flac",
])

/** Types never served inline regardless of ?preview=1 — mirrors the backend's
 *  dangerousContentTypes list in wiki_file_controller.go. */
export const DANGEROUS_CONTENT_TYPES = new Set<string>([
  "text/html",
  "application/xhtml+xml",
  "application/javascript",
  "application/x-javascript",
  "text/javascript",
])

/** Strips any parameters (e.g. "; charset=utf-8") and lowercases, so the
 *  comparison matches the backend's canonicalContentType. */
export function canonicalContentType(contentType: string | null | undefined): string {
  return (contentType ?? "").split(";")[0].trim().toLowerCase()
}

/** Whether the server sends the type inline under ?preview=1. */
export function servedInline(contentType: string): boolean {
  return PREVIEW_ALLOWED_CONTENT_TYPES.has(contentType) && !DANGEROUS_CONTENT_TYPES.has(contentType)
}

export interface FilePreviewKind {
  /** A PDF, in an iframe pointed at the inline URL. */
  isPdf: boolean
  /** A picture: drawn by the browser, or decoded first; null for none. */
  imageFormat: ImagePreviewFormat | null
  /** Video or audio, in a native player; null for none. */
  mediaKind: MediaPreviewKind | null
  /** Converted into a sandboxed frame (source.ts); null for none. */
  renderedKind: InlinePreviewKind | null
}

/** How a file with this (canonical) content type, name and size previews. */
export function filePreviewKind(contentType: string, filename: string, size: number): FilePreviewKind {
  const isPdf = contentType === "application/pdf"
  const imageFormat = isPdf ? null : imagePreviewFormat(contentType, filename)
  const mediaKind = isPdf || imageFormat !== null ? null : detectMediaPreviewKind(contentType, filename)
  const renderedKind =
    isPdf || imageFormat !== null || mediaKind !== null ? null : detectInlinePreviewKind(contentType, filename, size)
  return { isPdf, imageFormat, mediaKind, renderedKind }
}

/** Whether the preview panel can show it: a PDF, media, or a converted document. */
export function panelPreviews(kind: FilePreviewKind): boolean {
  return kind.isPdf || kind.mediaKind !== null || kind.renderedKind !== null
}

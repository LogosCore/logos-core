// Decides whether an attachment is video or audio the browser plays natively.
//
// Like images (wiki-file-preview-image.ts), media never travels the fetch and
// convert pipeline in wiki-file-preview-source.ts: the card points a <video>
// or <audio> at the attachment URL and the browser streams what it needs,
// seeking with Range requests the backend answers.

export type MediaPreviewKind = "video" | "audio"

/** Containers browsers open. Mirrors the media entries in
 *  previewAllowedContentTypes in wiki_file_controller.go, which must serve them
 *  inline for the player to load.
 *
 *  A container says nothing certain about the codec inside. QuickTime is the
 *  clearest case: an iPhone or a screen recorder writes H.264 or HEVC, which
 *  play, and an older or pro export may be ProRes, which plays nowhere. So a
 *  .mov gets a player like the rest, and the player reports a file it cannot
 *  decode (FilePreviewPanel in wiki-file-card.tsx) instead of sitting black. */
const VIDEO_CONTENT_TYPES = new Set<string>([
  "video/webm",
  "video/mp4",
  "video/ogg",
  "video/quicktime",
  "video/x-m4v",
])
const AUDIO_CONTENT_TYPES = new Set<string>([
  "audio/mpeg",
  "audio/ogg",
  "audio/wav",
  "audio/x-wav",
  "audio/webm",
  "audio/mp4",
  "audio/flac",
])

/** Extension fallback for when sniffing lands on something generic. */
const VIDEO_EXTENSIONS = new Set<string>(["webm", "mp4", "m4v", "ogv", "mov"])
const AUDIO_EXTENSIONS = new Set<string>([
  "mp3",
  "ogg",
  "oga",
  "wav",
  "weba",
  "m4a",
  "flac",
])

function extensionOf(filename: string): string {
  const dot = filename.lastIndexOf(".")
  return dot === -1 ? "" : filename.slice(dot + 1).toLowerCase()
}

/**
 * Returns "video" or "audio" for an attachment the browser can play inline,
 * or null. Content type wins; the extension is consulted only when the type
 * is unrecognised, never to override one the backend identified.
 */
export function detectMediaPreviewKind(
  contentType: string,
  filename: string,
): MediaPreviewKind | null {
  if (VIDEO_CONTENT_TYPES.has(contentType)) return "video"
  if (AUDIO_CONTENT_TYPES.has(contentType)) return "audio"
  if (contentType.startsWith("video/") || contentType.startsWith("audio/")) {
    // A type the backend named but browsers do not reliably decode (say
    // video/quicktime): offer the download, not a player that stays black.
    return null
  }
  const ext = extensionOf(filename)
  if (VIDEO_EXTENSIONS.has(ext)) return "video"
  if (AUDIO_EXTENSIONS.has(ext)) return "audio"
  return null
}

import type { Editor } from "@tiptap/core"

// What the wiki's file and image uploads share (wiki-file-upload.ts,
// wiki-image-upload.ts).

/** A document position kept inside the document. */
export function clampToDocSize(editor: Editor, pos: number): number {
  const size = editor.state.doc.content.size
  if (pos < 0) return 0
  if (pos > size) return size
  return pos
}

/** A file's size as the upload toasts say it: B, KB or MB. */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

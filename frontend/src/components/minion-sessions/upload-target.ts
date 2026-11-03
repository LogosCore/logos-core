// Where an upload from a listing goes (upload-dialog.tsx).

/** A folder on the host, as a listing writes it, and the names in it when
 *  the listing shows them. */
export interface UploadTarget {
  folder: string
  sep: string
  names?: readonly string[]
}

/** The path of name in the target's folder. */
export function joinPath(t: Pick<UploadTarget, "folder" | "sep">, name: string): string {
  return t.folder.endsWith(t.sep) ? t.folder + name : t.folder + t.sep + name
}

/**
 * Whether two paths on the target's host name the same file: on Windows (a
 * \ host) whatever their case, and / written for \, as Windows takes them.
 */
export function samePathOn(t: Pick<UploadTarget, "sep">, a: string, b: string): boolean {
  if (t.sep !== "\\") return a === b
  const norm = (p: string) => p.replaceAll("/", "\\").toLowerCase()
  return norm(a) === norm(b)
}

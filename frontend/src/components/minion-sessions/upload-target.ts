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

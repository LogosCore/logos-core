// The HTML export's stand-in for wiki-image-decode.ts.
//
// The viewer is one inlined script per bundle, so the app's decoders would
// ride along in every export: libheif alone is about 1.5 MB, for a format
// most exports never carry. A TIFF or HEIC attachment keeps its type icon and
// its download link here instead.

export function decodeImage(): Promise<string> {
  return Promise.reject(new Error("not decoded in the export"))
}

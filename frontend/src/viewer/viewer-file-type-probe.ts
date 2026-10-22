// The HTML export's stand-in for wiki-file-type-probe.ts. The export has no
// server to ask what a file is, so a card keeps the type its node carries.

export function useServerContentType(_url: string, contentType: string): string {
  return contentType
}

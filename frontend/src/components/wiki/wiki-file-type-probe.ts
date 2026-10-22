// What the server says a file is, for a card that does not know.
//
// A file card carries a content type from wherever the node was made. An
// upload through the editor records the server's answer; a card written as
// Markdown (an agent, an import, a paste) has only a guess from its extension,
// and a README or a .py guesses application/octet-stream. The card then shows
// a generic icon and offers a download, for a file the server would have
// called text/plain and the card could have previewed.
//
// So a card that only knows octet-stream asks, once: the first byte of the
// file, for the Content-Type the server sends with it. The answer is kept by
// URL for the life of the page. Nothing is written back to the document;
// every reader's card asks for itself.
//
// The HTML export's viewer has no server to ask and replaces this module with
// viewer-file-type-probe.ts (see vite.viewer.config.ts).

import { useEffect, useState } from "react"

const GENERIC = "application/octet-stream"

const answers = new Map<string, Promise<string | null>>()

/** The server's content type for url, or null when it has nothing better
 *  than octet-stream to say. Asked once per URL. */
export function probeContentType(url: string): Promise<string | null> {
  let answer = answers.get(url)
  if (!answer) {
    answer = fetch(url, { credentials: "same-origin", headers: { Range: "bytes=0-0" } })
      .then((res) => {
        // Only the headers are wanted; drop the byte.
        void res.body?.cancel()
        if (!res.ok) return null
        const type = (res.headers.get("Content-Type") ?? "").split(";")[0].trim().toLowerCase()
        return type === "" || type === GENERIC ? null : type
      })
      .catch(() => null)
    answers.set(url, answer)
  }
  return answer
}

/**
 * The content type to show this card by: the node's own, or the server's when
 * the node's says nothing. Starts as the node's and updates when the answer
 * arrives.
 */
export function useServerContentType(url: string, contentType: string): string {
  const unknown = url !== "" && (contentType === "" || contentType === GENERIC)
  const [answer, setAnswer] = useState<{ url: string; type: string } | null>(null)

  useEffect(() => {
    if (!unknown) return
    let cancelled = false
    void probeContentType(url).then((type) => {
      if (!cancelled && type !== null) setAnswer({ url, type })
    })
    return () => {
      cancelled = true
    }
  }, [unknown, url])

  return unknown && answer?.url === url ? answer.type : contentType
}

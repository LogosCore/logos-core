import { useEffect, useState } from "react"
import { formatBytes } from "@/lib/format-bytes"

// A text file's text while it comes (file-view.tsx): read from core as its
// bytes come (?follow=1), shown as it grows, up to MAX_SHOWN; its preview
// proper once the file is kept. The parent keys it by its URL: a new file
// reads afresh.

const MAX_SHOWN = 1 << 20

type Reading = "reading" | "cut" | "ended" | "failed"

export function LiveTextPane({ url }: { url: string }) {
  const [text, setText] = useState("")
  const [reading, setReading] = useState<Reading>("reading")
  useEffect(() => {
    const abort = new AbortController()
    let shown = ""
    let timer: number | null = null
    // At most ten updates a second, however fast it comes.
    const show = () => {
      timer = null
      setText(shown)
    }
    void (async () => {
      try {
        const res = await fetch(url, { credentials: "same-origin", signal: abort.signal })
        if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`)
        const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
        for (;;) {
          const { value, done } = await reader.read()
          if (done) break
          shown += value.slice(0, MAX_SHOWN - shown.length)
          if (timer === null) timer = window.setTimeout(show, 100)
          if (shown.length >= MAX_SHOWN) {
            abort.abort()
            show()
            setReading("cut")
            return
          }
        }
        show()
        setReading("ended")
      } catch {
        if (!abort.signal.aborted) setReading("failed")
      }
    })()
    return () => {
      abort.abort()
      if (timer !== null) window.clearTimeout(timer)
    }
  }, [url])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <pre className="min-h-0 flex-1 overflow-auto p-3 font-mono text-xs break-all whitespace-pre-wrap">{text}</pre>
      <div className="border-t px-3 py-1 text-xs text-muted-foreground">
        {reading === "reading"
          ? "As it comes: not checked yet."
          : reading === "cut"
            ? `The first ${formatBytes(MAX_SHOWN)} as it came; the whole once the file is kept.`
            : reading === "ended"
              ? "All of it has come: shown checked once kept."
              : "The file stopped coming."}
      </div>
    </div>
  )
}

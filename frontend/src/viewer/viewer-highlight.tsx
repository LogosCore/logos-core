// Search matches marked in the sidebar's results. The open page is painted
// by viewer-page-highlight.ts.

import { matchRanges } from "./viewer-data"

/** Text with the words' matches wrapped in <mark>. */
export function Highlighted({ text, words }: { text: string; words: string[] }) {
  const ranges = matchRanges(text, words)
  if (ranges.length === 0) return <>{text}</>
  const parts: React.ReactNode[] = []
  let at = 0
  ranges.forEach(([start, end], i) => {
    if (start > at) parts.push(text.slice(at, start))
    parts.push(
      <mark key={i} className="viewer-search-mark">
        {text.slice(start, end)}
      </mark>,
    )
    at = end
  })
  if (at < text.length) parts.push(text.slice(at))
  return <>{parts}</>
}

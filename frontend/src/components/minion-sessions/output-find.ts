// Finding text in a task's output (Ctrl+F in the console): where a query
// occurs, ignoring case, and the output cut at those places to mark them.

/** At most this many are marked: an output can be 4 MiB of one letter. */
export const MAX_MATCHES = 5000

/** Where query occurs in text, ignoring case: the start of each, not overlapping. */
export function findMatches(text: string, query: string, max = MAX_MATCHES): number[] {
  if (query === "") return []
  const hay = text.toLowerCase()
  const needle = query.toLowerCase()
  const out: number[] = []
  for (let i = hay.indexOf(needle); i !== -1 && out.length < max; i = hay.indexOf(needle, i + needle.length)) {
    out.push(i)
  }
  return out
}

export interface Segment {
  text: string
  /** The match's index, for one that is a match. */
  match?: number
}

/** The text in pieces: between matches, and each match. */
export function segments(text: string, starts: readonly number[], length: number): Segment[] {
  const out: Segment[] = []
  let at = 0
  starts.forEach((s, i) => {
    if (s > at) out.push({ text: text.slice(at, s) })
    out.push({ text: text.slice(s, s + length), match: i })
    at = s + length
  })
  if (at < text.length) out.push({ text: text.slice(at) })
  return out
}

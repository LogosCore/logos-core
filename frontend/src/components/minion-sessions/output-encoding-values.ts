// The Encodings table's logic: a piece of a task's output read in every
// encoding core knows (MinionTask.outputDecodings), for an operator to pick
// the one the session's output is read as.

export interface OutputDecoding {
  encoding: string
  name: string
  text: string
  unreadable: number
}

/** UTF-8, the encoding of a session without one. */
export const UTF8 = "utf-8"

/** The encoding's name, from the table's rows; its id when they lack it. */
export function encodingName(decodings: readonly OutputDecoding[], encoding: string | null | undefined): string {
  const id = encoding || UTF8
  return decodings.find((d) => d.encoding === id)?.name ?? id.toUpperCase()
}

/**
 * The rows in the order the table shows them: those with the fewest
 * characters that cannot be read first (the likely ones), core's order
 * among equals. A filter keeps those whose name or id has it.
 */
export function orderDecodings(decodings: readonly OutputDecoding[], filter: string): OutputDecoding[] {
  const f = filter.trim().toLowerCase()
  return decodings
    .map((d, i) => ({ d, i }))
    .filter(({ d }) => f === "" || d.name.toLowerCase().includes(f) || d.encoding.includes(f))
    .sort((a, b) => a.d.unreadable - b.d.unreadable || a.i - b.i)
    .map(({ d }) => d)
}

/** One row of the table: the encodings that read the piece alike. */
export interface DecodingGroup {
  /** What picking the row keeps: the session's own when it is one of them, else the first. */
  encoding: string
  name: string
  /** All of them, the row's first. */
  members: OutputDecoding[]
  text: string
  unreadable: number
}

/**
 * The rows as orderDecodings orders them, those that read the piece the
 * same as one (plain ASCII reads alike in every 8-bit encoding): the
 * session's encoding stands for its row, else the first.
 */
export function groupDecodings(decodings: readonly OutputDecoding[], filter: string, current: string): DecodingGroup[] {
  const groups = new Map<string, OutputDecoding[]>()
  for (const d of orderDecodings(decodings, filter)) {
    const g = groups.get(d.text)
    if (g) g.push(d)
    else groups.set(d.text, [d])
  }
  return [...groups.values()].map((members) => {
    const lead = members.find((d) => d.encoding === current) ?? members[0]
    return {
      encoding: lead.encoding,
      name: lead.name,
      members: [lead, ...members.filter((d) => d !== lead)],
      text: lead.text,
      unreadable: lead.unreadable,
    }
  })
}

/**
 * Text as shown: control characters, which show as nothing, as their
 * Control Pictures (␀ for a NUL: UTF-16 read a byte a character), and a
 * CR before a line feed dropped. Tabs and line feeds stay.
 */
export function visibleText(text: string): string {
  let out = ""
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i)
    if (c === 0x0d && text.charCodeAt(i + 1) === 0x0a) continue
    if (c === 0x09 || c === 0x0a) out += text[i]
    else if (c < 0x20) out += String.fromCharCode(0x2400 + c)
    else if (c === 0x7f) out += "␡"
    else if (c >= 0x80 && c <= 0x9f) out += "·"
    else out += text[i]
  }
  return out
}

/** The first line with something on it, for the table's one-line cell. */
export function firstLine(text: string): string {
  return (
    visibleText(text)
      .split("\n")
      .find((l) => l.trim() !== "") ?? ""
  )
}

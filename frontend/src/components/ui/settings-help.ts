// A setting's help as a settings row shows it: its first sentence under the
// control, the whole of it behind the info icon by the label. Modules write
// their settings' help as sentences, the first saying what the setting is.

const SENTENCE_END = /[.!?]\s+(?=[A-Z0-9"“])/g
// Abbreviations a capital may follow without a new sentence ("e.g. Cloudflare").
const ABBREVIATION = /\b(?:e\.g|i\.e|etc|vs)\.$/i

/** The help's first sentence, and the rest of it ("" when there is none). */
export function splitHelp(help: string): [lead: string, rest: string] {
  const text = help.trim()
  SENTENCE_END.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = SENTENCE_END.exec(text)) !== null) {
    const end = m.index + 1
    if (ABBREVIATION.test(text.slice(0, end))) continue
    return [text.slice(0, end), text.slice(end).trim()]
  }
  return [text, ""]
}

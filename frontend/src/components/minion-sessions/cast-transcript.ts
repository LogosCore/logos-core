import { Terminal } from "@xterm/headless"
import { parseSize, type Cast } from "./cast-values"

// A terminal's recording as text (a chunk of its own, cast-transcript-code.ts):
// what its process wrote played through a terminal of xterm.js's, without
// a screen, and its lines read back, as the terminal showed them,
// scrollback and all, without colours. Full-screen programs (an editor,
// top) draw on the terminal's other screen: their screens are not in it,
// what the shell wrote around them is.

/** The most lines a transcript keeps: past them, its first ones go. */
export const MAX_TRANSCRIPT_LINES = 500_000

/** A recording's transcript: its lines, a newline after each. */
export async function castTranscript(cast: Cast): Promise<string> {
  const { width, height } = cast.header
  let output = 0
  let newlines = 0
  for (const e of cast.events) {
    if (e.code !== "o") continue
    output += e.data.length
    for (let i = e.data.indexOf("\n"); i >= 0; i = e.data.indexOf("\n", i + 1)) newlines++
  }
  // Room for every line it can have written: each newline, and each
  // terminal's width of text wrapped.
  const scrollback = Math.min(MAX_TRANSCRIPT_LINES, newlines + Math.ceil(output / Math.max(width, 1)) + height)
  const term = new Terminal({ cols: width, rows: height, scrollback, allowProposedApi: true })
  try {
    const write = (data: string) => new Promise<void>((resolve) => term.write(data, resolve))
    let pending = ""
    for (const e of cast.events) {
      if (e.code === "o") {
        pending += e.data
      } else if (e.code === "r") {
        const size = parseSize(e.data)
        if (!size) continue
        await write(pending)
        pending = ""
        term.resize(size.cols, size.rows)
      }
    }
    await write(pending)
    const buffer = term.buffer.normal
    const lines: string[] = []
    for (let i = 0; i < buffer.length; i++) {
      const line = buffer.getLine(i)
      if (!line) continue
      const text = line.translateToString(true)
      if (line.isWrapped && lines.length > 0) lines[lines.length - 1] += text
      else lines.push(text)
    }
    while (lines.length > 0 && lines[lines.length - 1] === "") lines.pop()
    return lines.length > 0 ? lines.join("\n") + "\n" : ""
  } finally {
    term.dispose()
  }
}

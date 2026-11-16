import { describe, expect, test } from "vitest"
import { castTranscript } from "./cast-transcript"
import { inputLines, parseCast, parseSize, playTimes, playedBy } from "./cast-values"

// A terminal's recording as core writes it (recordings/terminal.go).
const CAST = [
  `{"version":2,"width":20,"height":3,"timestamp":1763290000,"title":"terminal · web: ann · stream 1"}`,
  `[0.000000,"o","$ "]`,
  `[1.500000,"i","ls -l\\r"]`,
  `[1.510000,"o","ls -l\\r\\ntotal 0\\r\\n$ "]`,
  `[30.000000,"i","vim\\u001b[A\\u007f\\u0003"]`,
  `[31.000000,"r","40x5"]`,
  `[31.5,"o","\\u001b[?1049hEDITOR SCREEN\\u001b[?1049l"]`,
  `[32.000000,"o","done"]`,
  `[32.5,"o","cut`,
].join("\n")

describe("a terminal's recording", () => {
  test("reads as its header and events, a cut last line passed over", () => {
    const cast = parseCast(CAST)
    expect(cast.header).toMatchObject({ version: 2, width: 20, height: 3 })
    expect(cast.events.map((e) => [e.at, e.code])).toEqual([
      [0, "o"],
      [1500, "i"],
      [1510, "o"],
      [30000, "i"],
      [31000, "r"],
      [31500, "o"],
      [32000, "o"],
    ])
    expect(() => parseCast(`{"version":1,"width":80,"height":24}`)).toThrow(/v2/)
    expect(() => parseCast("<html>")).toThrow(/not an asciicast/)
  })

  test("plays its pauses, or skips those past a bound", () => {
    const events = parseCast(CAST).events
    expect(playTimes(events, null)).toEqual([0, 1500, 1510, 30000, 31000, 31500, 32000])
    expect(playTimes(events, 2000)).toEqual([0, 1500, 1510, 3510, 4510, 5010, 5510])
    const times = playTimes(events, null)
    expect([playedBy(times, -1), playedBy(times, 0), playedBy(times, 1509), playedBy(times, 99999)]).toEqual([0, 1, 2, 7])
  })

  test("says what was typed, line by line, its keys named", () => {
    const events = parseCast(CAST).events
    const lines = inputLines(events, playTimes(events, 2000))
    expect(lines).toEqual([
      { at: 1500, text: "ls -l⏎" },
      { at: 3510, text: "vim<Up><Backspace><Ctrl+C>" },
    ])
    const keys = inputLines([{ at: 0, code: "i", data: "\t\x1b\x1b[3~\x1bOD\x1b[1;5C\x1c" }], [0])
    expect(keys[0].text).toBe("<Tab><Esc><Delete><Left><Esc[1;5C><Ctrl+\\>")
  })

  test("reads a win32-input-mode client's keys: each press, as typed", () => {
    // As Win11's recording has them: a Windows console asked for the mode
    // (ESC[?9001h), and the client sent each key's press and release.
    const key = (vk: number, uc: number, down: number, cs = 0, rc = 1) => `\x1b[${vk};0;${uc};${down};${cs};${rc}_`
    const typed = (word: string) => [...word].flatMap((c) => [key(c.toUpperCase().charCodeAt(0), c.charCodeAt(0), 1), key(c.toUpperCase().charCodeAt(0), c.charCodeAt(0), 0)])
    const events = [...typed("whoami"), key(13, 13, 1), key(13, 13, 0), ...typed("exit"), key(13, 13, 1)].map((data, i) => ({
      at: 2000 + i * 100,
      code: "i",
      data,
    }))
    const lines = inputLines(events, playTimes(events, null))
    expect(lines).toEqual([
      { at: 2000, text: "whoami⏎" },
      { at: 3400, text: "exit⏎" },
    ])
    // Shift alone types nothing, with Shift its character; Ctrl+C, Alt+x,
    // AltGr's own character; named keys; a key held; one event holding
    // several keys; fields left out.
    const data =
      key(0x10, 0, 1, 0x10) + key(0x41, 65, 1, 0x10) + key(0x43, 3, 1, 0x08) + key(0x58, 120, 1, 0x02) + key(0x51, 64, 1, 0x0a) +
      key(0x26, 0, 1) + key(0x74, 0, 1) + key(0x2e, 0, 1, 0x100) + key(0x08, 8, 1, 0, 3) + key(0x09, 9, 1) + key(0x1b, 27, 1) + "\x1b[;;98;1_"
    expect(inputLines([{ at: 0, code: "i", data }], [0])[0].text).toBe(
      "A<Ctrl+C><Alt+x>@<Up><F5><Delete><Backspace><Backspace><Backspace><Tab><Esc>b",
    )
    // Releases alone type nothing: no line.
    expect(inputLines([{ at: 0, code: "i", data: key(0x41, 97, 0) }], [0])).toEqual([])
  })

  test("reads its sizes", () => {
    expect(parseSize("120x40")).toEqual({ cols: 120, rows: 40 })
    expect(parseSize("0x40")).toBeNull()
    expect(parseSize("big")).toBeNull()
  })

  test("is a text: the lines the terminal showed, wrapped ones joined, a full-screen program's screen left out", async () => {
    const cast = parseCast(CAST)
    cast.events.splice(2, 0, { at: 1505, code: "o", data: "a line longer than twenty columns\r\n" })
    expect(await castTranscript(cast)).toBe("$ a line longer than twenty columns\nls -l\ntotal 0\n$ done\n")
  })

  test("keeps every line past the terminal's height", async () => {
    const rows = Array.from({ length: 50 }, (_, i) => `row ${i}\r\n`).join("")
    const text = await castTranscript({ header: { version: 2, width: 10, height: 2 }, events: [{ at: 0, code: "o", data: rows }] })
    expect(text.split("\n").length).toBe(51)
    expect(text.startsWith("row 0\nrow 1\n")).toBe(true)
  })
})

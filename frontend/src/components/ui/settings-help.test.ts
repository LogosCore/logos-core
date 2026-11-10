import { describe, expect, test } from "vitest"
import { splitHelp } from "./settings-help"

describe("splitHelp", () => {
  test("the first sentence, then the rest", () => {
    expect(
      splitHelp(
        "Where the minion connects: http(s)://host, with a port when it is not the scheme's default. No path: the channel accepts any path.",
      ),
    ).toEqual([
      "Where the minion connects: http(s)://host, with a port when it is not the scheme's default.",
      "No path: the channel accepts any path.",
    ])
  })

  test("one sentence is all lead", () => {
    expect(splitHelp("User-Agent to send.")).toEqual(["User-Agent to send.", ""])
    expect(splitHelp("  No full stop  ")).toEqual(["No full stop", ""])
    expect(splitHelp("")).toEqual(["", ""])
  })

  test("an abbreviation or a lower-case word goes on with the sentence", () => {
    expect(splitHelp("Other fronts (e.g. Cloudflare) of it. A minion falls back to them.")).toEqual([
      "Other fronts (e.g. Cloudflare) of it.",
      "A minion falls back to them.",
    ])
    expect(splitHelp("Outbound proxy, e.g. http://proxy.corp:3128. Empty: none.")).toEqual([
      "Outbound proxy, e.g. http://proxy.corp:3128.",
      "Empty: none.",
    ])
  })

  test("a sentence may start with a quote or a number", () => {
    expect(splitHelp('Also wait this long. "0s": the count alone decides.')).toEqual([
      "Also wait this long.",
      '"0s": the count alone decides.',
    ])
    expect(splitHelp("Tasks at once. 0 runs none.")).toEqual(["Tasks at once.", "0 runs none."])
  })
})

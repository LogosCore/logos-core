import { describe, expect, it } from "vitest"
import { problemsText, shownState } from "./target-values"

describe("a target's shown state", () => {
  it("is needs editing once its values no longer fit, whatever the last call said", () => {
    expect(shownState({ state: "connected", problems: [{ key: "token", message: "required" }] })).toBe("needs_editing")
    expect(shownState({ state: "connected", problems: [] })).toBe("connected")
  })
  it("says its problems in one line, the target's own without a key", () => {
    expect(
      problemsText({
        problems: [
          { key: null, message: "no dumbo channel declares bind-call connections now" },
          { key: "token", message: "required" },
        ],
      }),
    ).toBe("no dumbo channel declares bind-call connections now; token: required")
  })
})

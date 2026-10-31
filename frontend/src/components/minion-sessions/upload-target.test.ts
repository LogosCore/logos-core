import { describe, expect, it } from "vitest"
import { joinPath } from "./upload-target"

describe("joinPath", () => {
  it("joins with the host's separator, once", () => {
    expect(joinPath({ folder: "/tmp", sep: "/" }, "a.bin")).toBe("/tmp/a.bin")
    expect(joinPath({ folder: "/", sep: "/" }, "a.bin")).toBe("/a.bin")
    expect(joinPath({ folder: "C:\\", sep: "\\" }, "a.bin")).toBe("C:\\a.bin")
    expect(joinPath({ folder: "C:\\Users", sep: "\\" }, "a.bin")).toBe("C:\\Users\\a.bin")
  })
})

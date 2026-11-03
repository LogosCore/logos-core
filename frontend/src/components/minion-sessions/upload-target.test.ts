import { describe, expect, it } from "vitest"
import { joinPath, samePathOn } from "./upload-target"

describe("joinPath", () => {
  it("joins with the host's separator, once", () => {
    expect(joinPath({ folder: "/tmp", sep: "/" }, "a.bin")).toBe("/tmp/a.bin")
    expect(joinPath({ folder: "/", sep: "/" }, "a.bin")).toBe("/a.bin")
    expect(joinPath({ folder: "C:\\", sep: "\\" }, "a.bin")).toBe("C:\\a.bin")
    expect(joinPath({ folder: "C:\\Users", sep: "\\" }, "a.bin")).toBe("C:\\Users\\a.bin")
  })
})

describe("samePathOn", () => {
  it("compares Windows paths whatever their case, / as a backslash; Unix ones exactly", () => {
    const win = { sep: "\\" }
    expect(samePathOn(win, "C:\\Users\\Report.docx", "c:\\users\\report.DOCX")).toBe(true)
    expect(samePathOn(win, "C:\\Users\\Report.docx", "C:/Users/Report.docx")).toBe(true)
    expect(samePathOn(win, "C:\\Users\\Report.docx", "C:\\Users\\Report.doc")).toBe(false)
    const unix = { sep: "/" }
    expect(samePathOn(unix, "/tmp/Report", "/tmp/report")).toBe(false)
    expect(samePathOn(unix, "/tmp/a", "/tmp/a")).toBe(true)
  })
})

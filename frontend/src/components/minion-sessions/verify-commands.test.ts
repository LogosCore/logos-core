import { describe, expect, it } from "vitest"
import { savedName, verifyCommands } from "./verify-commands"

describe("verifyCommands", () => {
  const sha = "A6F470CA" + "0".repeat(56)
  it("checks the file by its name, the hash as each tool wants it", () => {
    const [linux, mac, win] = verifyCommands("Win10.iso", sha)
    expect(linux.command).toBe(`echo 'a6f470ca${"0".repeat(56)}  Win10.iso' | sha256sum -c -`)
    expect(mac.command).toBe(`echo 'a6f470ca${"0".repeat(56)}  Win10.iso' | shasum -a 256 -c -`)
    expect(win.command).toContain(`-LiteralPath 'Win10.iso'`)
    expect(win.command).toContain(`-eq '${sha}'`)
  })
  it("quotes names with quotes and spaces", () => {
    const [linux, , win] = verifyCommands("it's here.bin", "ab")
    expect(linux.command).toBe(`echo 'ab  it'\\''s here.bin' | sha256sum -c -`)
    expect(win.command).toContain(`-LiteralPath 'it''s here.bin'`)
  })
})

describe("savedName", () => {
  it("is core's: the last part, other characters as _", () => {
    expect(savedName("/home/u/Win10 22H2 (x64).iso", "f")).toBe("Win10_22H2_x64_.iso")
    expect(savedName("C:\\Users\\a\\notes.txt", "f")).toBe("notes.txt")
    expect(savedName("/tmp/", "file-1")).toBe("file-1")
  })
})
